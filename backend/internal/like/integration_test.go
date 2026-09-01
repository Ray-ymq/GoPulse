//go:build integration

package like

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

func TestIntegrationLikeIdempotenceConcurrencyAggregatesAndForeignKeys(t *testing.T) {
	cfg := integrationtest.Environment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error=%v", err)
	}
	defer database.Close()
	releasePostFactsLock := integrationtest.AcquirePostFactsLock(t, database)
	defer releasePostFactsLock()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	firstUsername := "LikeA_" + suffix
	secondUsername := "LikeB_" + suffix
	firstUserID := insertLikeIntegrationUser(t, ctx, database, firstUsername)
	secondUserID := insertLikeIntegrationUser(t, ctx, database, secondUsername)
	postID := insertLikeIntegrationPost(t, ctx, database, firstUserID)
	t.Cleanup(func() {
		cleanupLikeIntegrationData(t, cfg, postID, firstUserID, secondUserID)
	})

	postService := post.NewService(post.NewMySQLRepository(database))
	repository := NewMySQLRepository(database)
	service := NewService(repository, postService)

	const concurrentRequests = 32
	start := make(chan struct{})
	errorsChannel := make(chan error, concurrentRequests)
	var workers sync.WaitGroup
	workers.Add(concurrentRequests)
	for range concurrentRequests {
		go func() {
			defer workers.Done()
			<-start
			errorsChannel <- service.Like(ctx, postID, firstUserID)
		}()
	}
	close(start)
	workers.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent Like() error=%v", err)
		}
	}
	assertLikeFactCount(t, ctx, database, postID, 1)

	if err := service.Like(ctx, postID, firstUserID); err != nil {
		t.Fatalf("repeated Like() error=%v", err)
	}
	assertLikeFactCount(t, ctx, database, postID, 1)

	if err := service.Like(ctx, postID, secondUserID); err != nil {
		t.Fatalf("second user Like() error=%v", err)
	}
	assertLikeFactCount(t, ctx, database, postID, 2)
	firstExists, err := service.Exists(ctx, postID, firstUserID)
	if err != nil || !firstExists {
		t.Fatalf("Exists(first)=%t error=%v", firstExists, err)
	}
	detail, err := postService.Detail(ctx, postID, firstUserID)
	if err != nil || detail.LikeCount != 2 || !detail.LikedByMe {
		t.Fatalf("Detail(first) post=%#v error=%v", detail, err)
	}

	if err := service.Unlike(ctx, postID, firstUserID); err != nil {
		t.Fatalf("Unlike() error=%v", err)
	}
	if err := service.Unlike(ctx, postID, firstUserID); err != nil {
		t.Fatalf("repeated Unlike() error=%v", err)
	}
	assertLikeFactCount(t, ctx, database, postID, 1)
	firstExists, err = service.Exists(ctx, postID, firstUserID)
	if err != nil || firstExists {
		t.Fatalf("Exists(first after delete)=%t error=%v", firstExists, err)
	}
	detail, err = postService.Detail(ctx, postID, firstUserID)
	if err != nil || detail.LikeCount != 1 || detail.LikedByMe {
		t.Fatalf("Detail(first after delete) post=%#v error=%v", detail, err)
	}
	detail, err = postService.Detail(ctx, postID, secondUserID)
	if err != nil || detail.LikeCount != 1 || !detail.LikedByMe {
		t.Fatalf("Detail(second) post=%#v error=%v", detail, err)
	}

	missingPostID := ^uint64(0)
	assertLikeCode(t, service.Like(ctx, missingPostID, firstUserID), apperror.CodePostNotFound)
	assertLikeCode(t, service.Unlike(ctx, missingPostID, firstUserID), apperror.CodePostNotFound)

	foreignKeyError := repository.Create(ctx, postID, ^uint64(0))
	if foreignKeyError == nil || errors.Is(foreignKeyError, ErrAlreadyExists) {
		t.Fatalf("Create() missing user error=%v, want preserved foreign-key failure", foreignKeyError)
	}
}

func insertLikeIntegrationUser(t *testing.T, ctx context.Context, database *sql.DB, username string) uint64 {
	t.Helper()
	result, err := database.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, "$2a$10$integration-placeholder")
	if err != nil {
		t.Fatalf("insert user %q: %v", username, err)
	}
	return likeInsertedID(t, result)
}

func insertLikeIntegrationPost(t *testing.T, ctx context.Context, database *sql.DB, authorID uint64) uint64 {
	t.Helper()
	result, err := database.ExecContext(ctx, `INSERT INTO posts (author_id, title, content) VALUES (?, ?, ?)`, authorID, "like integration", "content")
	if err != nil {
		t.Fatalf("insert post: %v", err)
	}
	return likeInsertedID(t, result)
}

func likeInsertedID(t *testing.T, result sql.Result) uint64 {
	t.Helper()
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		t.Fatalf("LastInsertId() id=%d error=%v", identifier, err)
	}
	return uint64(identifier)
}

func assertLikeFactCount(t *testing.T, ctx context.Context, database *sql.DB, postID uint64, want int) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM post_likes WHERE post_id = ?`, postID).Scan(&count); err != nil {
		t.Fatalf("count post likes: %v", err)
	}
	if count != want {
		t.Fatalf("post like count=%d, want %d", count, want)
	}
}

func cleanupLikeIntegrationData(t *testing.T, cfg config.Config, postID, firstUserID, secondUserID uint64) {
	t.Helper()
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Errorf("cleanup OpenMySQLDatabase() error=%v", err)
		return
	}
	defer database.Close()
	if _, err := database.ExecContext(context.Background(), `DELETE FROM post_likes WHERE post_id = ? OR user_id IN (?, ?)`, postID, firstUserID, secondUserID); err != nil {
		t.Errorf("cleanup post likes: %v", err)
	}
	if _, err := database.ExecContext(context.Background(), `DELETE FROM posts WHERE id = ?`, postID); err != nil {
		t.Errorf("cleanup post: %v", err)
	}
	if _, err := database.ExecContext(context.Background(), `DELETE FROM users WHERE id IN (?, ?)`, firstUserID, secondUserID); err != nil {
		t.Errorf("cleanup users: %v", err)
	}
}

func assertLikeCode(t *testing.T, err error, code apperror.Code) {
	t.Helper()
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != code {
		t.Fatalf("error=%#v, want code %q", err, code)
	}
}
