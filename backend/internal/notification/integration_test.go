//go:build integration

package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationRepositoryCreatesBothNotificationShapesAndAbsorbsConcurrentDuplicates(t *testing.T) {
	cfg := integrationtest.Environment(t)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	release := integrationtest.AcquirePostFactsLock(t, database)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	assertNotificationSchema(t, ctx, database)
	actorID := insertUser(t, ctx, database, "notification_actor")
	recipientID := insertUser(t, ctx, database, "notification_recipient")
	postID := insertPost(t, ctx, database, recipientID)
	commentID := insertComment(t, ctx, database, postID, actorID)
	defer cleanupNotificationFixture(t, database, postID, actorID, recipientID)
	repository, err := NewRepository(database)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	baseTime := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	commentEvent, _ := bus.NewCommentCreated(baseTime, actorID, recipientID, postID, commentID)
	likeEvent, _ := bus.NewPostLiked(baseTime.Add(time.Second), actorID, recipientID, postID)

	const duplicates = 2
	start := make(chan struct{})
	results := make(chan error, duplicates)
	var group sync.WaitGroup
	for range duplicates {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, insertErr := repository.Insert(ctx, commentEvent)
			results <- insertErr
		}()
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent Insert() error = %v", err)
		}
	}
	if created, err := repository.Insert(ctx, likeEvent); err != nil || !created {
		t.Fatalf("Insert(like) created=%t error=%v", created, err)
	}
	self, _ := bus.NewPostLiked(time.Now().UTC(), recipientID, recipientID, postID)
	if created, err := repository.Insert(ctx, self); err != nil || created {
		t.Fatalf("Insert(self) created=%t error=%v", created, err)
	}
	assertNotificationCount(t, ctx, database, commentEvent.EventID, 1)
	assertNotificationCount(t, ctx, database, likeEvent.EventID, 1)
	commentRecord, err := repository.FindBySourceEventID(ctx, commentEvent.EventID)
	if err != nil || commentRecord.CommentID == nil || *commentRecord.CommentID != commentID || commentRecord.ReadAt != nil {
		t.Fatalf("comment notification=%#v error=%v", commentRecord, err)
	}
	likeRecord, err := repository.FindBySourceEventID(ctx, likeEvent.EventID)
	if err != nil || likeRecord.CommentID != nil {
		t.Fatalf("like notification=%#v error=%v", likeRecord, err)
	}

	firstPage, err := NewService(repository).List(ctx, recipientID, ListOptions{Limit: 1})
	if err != nil || len(firstPage.Notifications) != 1 || firstPage.NextCursor == nil || firstPage.Notifications[0].ID != likeRecord.ID {
		t.Fatalf("first recipient page=%#v error=%v", firstPage, err)
	}
	cursor, err := DecodeCursor(*firstPage.NextCursor)
	if err != nil {
		t.Fatalf("DecodeCursor() error=%v", err)
	}
	secondPage, err := NewService(repository).List(ctx, recipientID, ListOptions{Limit: 1, Cursor: &cursor})
	if err != nil || len(secondPage.Notifications) != 1 || secondPage.Notifications[0].ID != commentRecord.ID {
		t.Fatalf("second recipient page=%#v error=%v", secondPage, err)
	}
	otherUserRecords, err := repository.ListByRecipient(ctx, actorID, ListOptions{Limit: 20})
	if err != nil || len(otherUserRecords) != 0 {
		t.Fatalf("other recipient records=%#v error=%v", otherUserRecords, err)
	}
	if err := repository.MarkRead(ctx, actorID, likeRecord.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MarkRead(other recipient) error=%v", err)
	}
	if err := repository.MarkRead(ctx, recipientID, likeRecord.ID); err != nil {
		t.Fatalf("MarkRead() error=%v", err)
	}
	readRecord, err := repository.FindBySourceEventID(ctx, likeEvent.EventID)
	if err != nil || readRecord.ReadAt == nil {
		t.Fatalf("read notification=%#v error=%v", readRecord, err)
	}
	originalReadAt := *readRecord.ReadAt
	if err := repository.MarkRead(ctx, recipientID, likeRecord.ID); err != nil {
		t.Fatalf("repeated MarkRead() error=%v", err)
	}
	repeatedRecord, err := repository.FindBySourceEventID(ctx, likeEvent.EventID)
	if err != nil || repeatedRecord.ReadAt == nil || !repeatedRecord.ReadAt.Equal(originalReadAt) {
		t.Fatalf("repeated read notification=%#v error=%v", repeatedRecord, err)
	}
}

func assertNotificationSchema(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	var tableName, createTable string
	if err := database.QueryRowContext(ctx, `SHOW CREATE TABLE notifications`).Scan(&tableName, &createTable); err != nil {
		t.Fatalf("SHOW CREATE TABLE notifications: %v", err)
	}
	for _, required := range []string{
		"uq_notifications_source_event_id", "idx_notifications_recipient_created_id",
		"fk_notifications_recipient", "fk_notifications_actor",
		"fk_notifications_post", "fk_notifications_comment",
	} {
		if !strings.Contains(createTable, required) {
			t.Fatalf("notifications schema missing %q: %s", required, createTable)
		}
	}
}

func insertUser(t *testing.T, ctx context.Context, database *sql.DB, prefix string) uint64 {
	t.Helper()
	result, err := database.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?)`, fmt.Sprintf("%.12s_%012d", prefix, time.Now().UnixNano()%1_000_000_000_000), "$2a$10$integration-placeholder")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, _ := result.LastInsertId()
	return uint64(id)
}
func insertPost(t *testing.T, ctx context.Context, database *sql.DB, authorID uint64) uint64 {
	t.Helper()
	result, err := database.ExecContext(ctx, `INSERT INTO posts (author_id, title, content) VALUES (?, 'notification integration', 'content')`, authorID)
	if err != nil {
		t.Fatalf("insert post: %v", err)
	}
	id, _ := result.LastInsertId()
	return uint64(id)
}
func insertComment(t *testing.T, ctx context.Context, database *sql.DB, postID, authorID uint64) uint64 {
	t.Helper()
	result, err := database.ExecContext(ctx, `INSERT INTO comments (post_id, author_id, content) VALUES (?, ?, 'integration')`, postID, authorID)
	if err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	id, _ := result.LastInsertId()
	return uint64(id)
}
func assertNotificationCount(t *testing.T, ctx context.Context, database *sql.DB, eventID string, want int) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE source_event_id = ?`, eventID).Scan(&count); err != nil || count != want {
		t.Fatalf("notification count=%d error=%v want=%d", count, err, want)
	}
}
func cleanupNotificationFixture(t *testing.T, database *sql.DB, postID, actorID, recipientID uint64) {
	t.Helper()
	ctx := context.Background()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM notifications WHERE post_id = ?`, []any{postID}}, {`DELETE FROM comments WHERE post_id = ?`, []any{postID}},
		{`DELETE FROM post_likes WHERE post_id = ?`, []any{postID}}, {`DELETE FROM posts WHERE id = ?`, []any{postID}},
		{`DELETE FROM users WHERE id IN (?, ?)`, []any{actorID, recipientID}},
	} {
		if _, err := database.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Errorf("cleanup fixture: %v", err)
		}
	}
}
