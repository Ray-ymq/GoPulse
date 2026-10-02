//go:build integration

package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	stdhttp "net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bookmark"
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/gin-gonic/gin"
)

type missingCommentOutbox struct{}

func (missingCommentOutbox) Insert(context.Context, outbox.Executor, bus.Envelope) error { return nil }

func TestIntegrationTransactionRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := integrationtest.Environment(t)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	insertUser := func(name string) uint64 {
		result, err := database.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?)`, name+suffix, "integration-placeholder")
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return uint64(id)
	}
	owner, actor := insertUser("TxnOwner"), insertUser("TxnActor")
	result, err := database.ExecContext(ctx, `INSERT INTO posts (author_id, title, content) VALUES (?, ?, ?)`, owner, "transaction recovery", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	postID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, statement := range []string{
			`DELETE FROM business_outbox WHERE JSON_EXTRACT(payload, '$.post_id') = ?`,
			`DELETE FROM comments WHERE post_id = ?`,
			`DELETE FROM post_bookmarks WHERE post_id = ?`,
			`DELETE FROM posts WHERE id = ?`,
		} {
			if _, err := database.ExecContext(cleanup, statement, postID); err != nil {
				t.Errorf("fixture cleanup: %v", err)
			}
		}
		if _, err := database.ExecContext(cleanup, `DELETE FROM users WHERE id IN (?, ?)`, owner, actor); err != nil {
			t.Errorf("user cleanup: %v", err)
		}
	})
	proxy := integrationtest.NewMySQLFaultProxy(t, net.JoinHostPort(cfg.MySQL.Host, strconv.Itoa(cfg.MySQL.Port)))
	proxyCfg := cfg.MySQL
	proxyCfg.Port = proxy.Port()
	proxied, err := platform.OpenMySQLDatabase(proxyCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxied.Close() })
	events, err := outbox.NewRepository(proxied, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	posts := post.NewService(post.NewMySQLRepository(proxied))
	newRouterFor := func(userID uint64, writer outbox.Writer) *gin.Engine {
		return NewRouter(Dependencies{}, APIRoutes{
			Comments:       comment.NewHandler(comment.NewService(comment.NewMySQLRepositoryWithOutbox(proxied, writer), posts)),
			Bookmarks:      bookmark.NewHandler(bookmark.NewService(bookmark.NewMySQLRepository(proxied), posts)),
			Authentication: middleware.RequireAuthentication("session", acceptingVerifier{userID: userID}),
		})
	}
	router := newRouterFor(actor, events)
	cookie := &stdhttp.Cookie{Name: "session", Value: "integration-token"}
	path := fmt.Sprintf("/api/v1/posts/%d", postID)
	countEvents := func() int {
		var count int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM business_outbox WHERE JSON_EXTRACT(payload, '$.post_id') = ?`, postID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	for _, test := range []struct {
		name              string
		query             string
		rollback          bool
		status            int
		self              bool
		omitEvent         bool
		facts, eventCount int
	}{
		{"comment_commit_reply_lost", "COMMIT", false, stdhttp.StatusCreated, false, false, 1, 1},
		{"comment_begin_reply_lost", "START TRANSACTION", false, stdhttp.StatusCreated, false, false, 1, 1},
		{"comment_uncommitted", "COMMIT", true, stdhttp.StatusInternalServerError, false, false, 0, 0},
		{"self_comment_commit_reply_lost", "COMMIT", false, stdhttp.StatusCreated, true, false, 1, 0},
		{"comment_event_missing", "COMMIT", false, stdhttp.StatusInternalServerError, false, true, 1, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := test.name + suffix
			body, _ := json.Marshal(comment.CreateInput{Content: content})
			beforeEvents := countEvents()
			requestActor := actor
			requestRouter := router
			if test.self {
				requestActor = owner
				requestRouter = newRouterFor(owner, events)
			} else if test.omitEvent {
				requestRouter = newRouterFor(actor, missingCommentOutbox{})
			}
			proxy.LoseNextReply(test.query, test.rollback)
			response := performJSONRequest(requestRouter, stdhttp.MethodPost, path+"/comments", string(body), cookie)
			if proxy.Faults() != 1 {
				t.Fatalf("fault count=%d, want one executed fault", proxy.Faults())
			}
			if response.Code != test.status {
				var persisted int
				if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE post_id = ? AND author_id = ? AND content = ?`, postID, requestActor, content).Scan(&persisted); err != nil {
					t.Fatal(err)
				}
				t.Fatalf("status=%d, want %d; persisted comments=%d, event delta=%d: %s", response.Code, test.status, persisted, countEvents()-beforeEvents, response.Body.String())
			}
			var count int
			if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE post_id = ? AND author_id = ? AND content = ?`, postID, requestActor, content).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if test.status == stdhttp.StatusCreated {
				var envelope struct{ Data comment.Comment }
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Data.ID == 0 {
					t.Fatalf("created response has no comment identity: %s", response.Body.String())
				}
				var associated int
				if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM business_outbox WHERE event_type = 'comment.created' AND JSON_EXTRACT(payload, '$.comment_id') = ?`, envelope.Data.ID).Scan(&associated); err != nil || associated != test.eventCount {
					t.Fatalf("response/event association=%d, error=%v", associated, err)
				}
			}
			if count != test.facts || countEvents()-beforeEvents != test.eventCount {
				t.Fatalf("facts=%d, want %d; event delta=%d, want %d", count, test.facts, countEvents()-beforeEvents, test.eventCount)
			}
		})
	}

	for _, test := range []struct {
		name, query, method string
		enabled             bool
	}{
		{"bookmark_begin_reply_lost", "START TRANSACTION", stdhttp.MethodPut, true},
		{"bookmark_commit_reply_lost", "COMMIT", stdhttp.MethodPut, true},
		{"unbookmark_commit_reply_lost", "COMMIT", stdhttp.MethodDelete, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := database.ExecContext(ctx, `DELETE FROM post_bookmarks WHERE post_id = ? AND user_id = ?`, postID, actor); err != nil {
				t.Fatal(err)
			}
			if !test.enabled {
				if _, err := database.ExecContext(ctx, `INSERT INTO post_bookmarks (post_id, user_id) VALUES (?, ?)`, postID, actor); err != nil {
					t.Fatal(err)
				}
			}
			beforeEvents := countEvents()
			proxy.LoseNextReply(test.query, false)
			response := performJSONRequest(router, test.method, path+"/bookmark", "", cookie)
			if proxy.Faults() != 1 || response.Code != stdhttp.StatusNoContent {
				t.Fatalf("faults=%d status=%d body=%s", proxy.Faults(), response.Code, response.Body.String())
			}
			var exists bool
			if err := database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM post_bookmarks WHERE post_id = ? AND user_id = ?)`, postID, actor).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if exists != test.enabled || countEvents() != beforeEvents {
				t.Fatalf("bookmark=%v, want %v; bookmark must not produce Outbox events", exists, test.enabled)
			}
			if repeated := performJSONRequest(router, test.method, path+"/bookmark", "", cookie); repeated.Code != stdhttp.StatusNoContent {
				t.Fatalf("idempotent repeat=%d", repeated.Code)
			}
		})
	}
}
