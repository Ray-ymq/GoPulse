//go:build integration

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/like"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

func TestIntegrationPostHTTPCreateListAndDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := integrationtest.Environment(t)
	suffix := time.Now().UTC().Format("150405000000")
	username := "PostHTTP_" + suffix
	otherUsername := "PostLike_" + suffix
	cleanupHTTPPostUsers(t, cfg, username, otherUsername)

	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	releasePostFactsLock := integrationtest.AcquirePostFactsLock(t, database)
	defer releasePostFactsLock()
	router := integrationPostRouter(t, cfg, database)

	registered := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/auth/register", `{"username":"`+username+`","password":"integration-password"}`, nil)
	if registered.Code != stdhttp.StatusCreated {
		t.Fatalf("register status=%d body=%s", registered.Code, registered.Body.String())
	}
	cookies := registered.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("register cookies=%#v", cookies)
	}
	var registration struct {
		Data user.Public `json:"data"`
	}
	decodeIntegrationResponse(t, registered, &registration)

	unknownAuthor := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/posts", `{"title":"title","content":"content","author_id":999}`, cookies[0])
	if unknownAuthor.Code != stdhttp.StatusBadRequest {
		t.Fatalf("unknown author status=%d body=%s", unknownAuthor.Code, unknownAuthor.Body.String())
	}

	created := performJSONRequest(router, stdhttp.MethodPost, "/api/v1/posts", `{"title":"  First post  ","content":"  Hello GoPulse  "}`, cookies[0])
	if created.Code != stdhttp.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var creation struct {
		Data post.Post `json:"data"`
	}
	decodeIntegrationResponse(t, created, &creation)
	if creation.Data.ID == 0 || creation.Data.Title != "First post" || creation.Data.Content != "Hello GoPulse" || creation.Data.Author.ID != registration.Data.ID || creation.Data.Author.Username != username {
		t.Fatalf("created post=%#v registration=%#v", creation.Data, registration.Data)
	}
	if creation.Data.CommentCount != 0 || creation.Data.LikeCount != 0 || creation.Data.LikedByMe {
		t.Fatalf("new post aggregates=%#v", creation.Data)
	}

	otherID := insertHTTPIntegrationUser(t, database, otherUsername)
	if _, err := database.Exec(`INSERT INTO comments (post_id, author_id, content) VALUES (?, ?, ?), (?, ?, ?)`, creation.Data.ID, registration.Data.ID, "viewer comment", creation.Data.ID, otherID, "other comment"); err != nil {
		t.Fatalf("insert comments: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO post_likes (post_id, user_id) VALUES (?, ?), (?, ?)`, creation.Data.ID, registration.Data.ID, creation.Data.ID, otherID); err != nil {
		t.Fatalf("insert likes: %v", err)
	}

	listed := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/posts?limit=1", "", cookies[0])
	if listed.Code != stdhttp.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	var listing struct {
		Data []post.Post `json:"data"`
		Meta struct {
			NextCursor *string `json:"next_cursor"`
		} `json:"meta"`
	}
	decodeIntegrationResponse(t, listed, &listing)
	if len(listing.Data) != 1 || listing.Data[0].ID != creation.Data.ID || listing.Meta.NextCursor != nil {
		t.Fatalf("listing=%#v", listing)
	}
	assertHTTPIntegrationPostAggregates(t, listing.Data[0])

	detail := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/posts/"+strconv.FormatUint(creation.Data.ID, 10), "", cookies[0])
	if detail.Code != stdhttp.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	var detailEnvelope struct {
		Data post.Post `json:"data"`
	}
	decodeIntegrationResponse(t, detail, &detailEnvelope)
	assertHTTPIntegrationPostAggregates(t, detailEnvelope.Data)

	missing := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/posts/18446744073709551615", "", cookies[0])
	if missing.Code != stdhttp.StatusNotFound {
		t.Fatalf("missing detail status=%d body=%s", missing.Code, missing.Body.String())
	}
	unauthenticated := performJSONRequest(router, stdhttp.MethodGet, "/api/v1/posts", "", nil)
	if unauthenticated.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("unauthenticated list status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}
}

func integrationPostRouter(t *testing.T, cfg config.Config, database *sql.DB) stdhttp.Handler {
	t.Helper()
	passwords, err := auth.NewPasswordManager()
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	tokens, err := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL, time.Now)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	cookies := auth.NewCookieManager(cfg.Auth.CookieName, cfg.Auth.CookieSecure, cfg.Auth.JWTTTL, time.Now)
	authService := auth.NewService(user.NewMySQLRepository(database), passwords, tokens)
	postService := post.NewService(post.NewMySQLRepository(database))
	commentService := comment.NewService(comment.NewMySQLRepository(database), postService)
	likeService := like.NewService(like.NewMySQLRepository(database), postService)
	return NewRouter(Dependencies{}, APIRoutes{
		Auth:           auth.NewHandler(authService, cookies),
		Posts:          post.NewHandler(postService),
		Comments:       comment.NewHandler(commentService),
		Likes:          like.NewHandler(likeService),
		Authentication: middleware.RequireAuthentication(cookies.Name(), tokens),
	})
}

func insertHTTPIntegrationUser(t *testing.T, database *sql.DB, username string) uint64 {
	t.Helper()
	result, err := database.Exec(`INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, "$2a$10$integration-placeholder")
	if err != nil {
		t.Fatalf("insert integration user: %v", err)
	}
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		t.Fatalf("LastInsertId() id=%d error=%v", identifier, err)
	}
	return uint64(identifier)
}

func assertHTTPIntegrationPostAggregates(t *testing.T, record post.Post) {
	t.Helper()
	if record.CommentCount != 2 || record.LikeCount != 2 || !record.LikedByMe {
		t.Fatalf("post aggregates=%#v", record)
	}
}

func decodeIntegrationResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func cleanupHTTPPostUsers(t *testing.T, cfg config.Config, usernames ...string) {
	t.Helper()
	t.Cleanup(func() {
		database, err := platform.OpenMySQLDatabase(cfg.MySQL)
		if err != nil {
			t.Errorf("cleanup OpenMySQLDatabase() error = %v", err)
			return
		}
		defer database.Close()
		placeholders := "?,?"
		arguments := []any{usernames[0], usernames[1], usernames[0], usernames[1]}
		queries := []string{
			`DELETE likes FROM post_likes AS likes LEFT JOIN posts AS p ON p.id = likes.post_id LEFT JOIN users AS author ON author.id = p.author_id LEFT JOIN users AS liker ON liker.id = likes.user_id WHERE author.username IN (` + placeholders + `) OR liker.username IN (` + placeholders + `)`,
			`DELETE comments FROM comments LEFT JOIN posts AS p ON p.id = comments.post_id LEFT JOIN users AS post_author ON post_author.id = p.author_id LEFT JOIN users AS comment_author ON comment_author.id = comments.author_id WHERE post_author.username IN (` + placeholders + `) OR comment_author.username IN (` + placeholders + `)`,
		}
		for _, query := range queries {
			if _, err := database.ExecContext(context.Background(), query, arguments...); err != nil {
				t.Errorf("cleanup related post data: %v", err)
			}
		}
		if _, err := database.ExecContext(context.Background(), `DELETE posts FROM posts INNER JOIN users ON users.id = posts.author_id WHERE users.username IN (`+placeholders+`)`, usernames[0], usernames[1]); err != nil {
			t.Errorf("cleanup posts: %v", err)
		}
		if _, err := database.ExecContext(context.Background(), `DELETE FROM users WHERE username IN (`+placeholders+`)`, usernames[0], usernames[1]); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
	})
}
