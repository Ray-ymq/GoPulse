//go:build integration

package post

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationPostRepositoryReadModelStablePaginationAndQueryPlan(t *testing.T) {
	cfg := integrationtest.Environment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	releasePostFactsLock := integrationtest.AcquirePostFactsLock(t, database)
	defer releasePostFactsLock()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer tx.Rollback()

	suffix := time.Now().UTC().Format("150405000000")
	viewerID := insertIntegrationUser(t, ctx, tx, "PostViewer_"+suffix)
	otherID := insertIntegrationUser(t, ctx, tx, "PostOther_"+suffix)
	base := time.Date(2026, 9, 1, 8, 0, 0, 100000, time.UTC)
	oldestID := insertIntegrationPost(t, ctx, tx, viewerID, "oldest", base)
	sameTimeOlderID := insertIntegrationPost(t, ctx, tx, otherID, "same older", base.Add(time.Minute))
	sameTimeNewerID := insertIntegrationPost(t, ctx, tx, viewerID, "same newer", base.Add(time.Minute))
	newestID := insertIntegrationPost(t, ctx, tx, otherID, "newest", base.Add(2*time.Minute))

	insertIntegrationComment(t, ctx, tx, sameTimeNewerID, viewerID, "first")
	insertIntegrationComment(t, ctx, tx, sameTimeNewerID, otherID, "second")
	insertIntegrationComment(t, ctx, tx, newestID, viewerID, "third")
	insertIntegrationLike(t, ctx, tx, sameTimeNewerID, viewerID)
	insertIntegrationLike(t, ctx, tx, sameTimeNewerID, otherID)
	insertIntegrationLike(t, ctx, tx, newestID, otherID)

	counting := &countingDatabase{database: tx}
	repository := NewMySQLRepository(counting)
	service := NewService(repository)
	first, err := service.List(ctx, viewerID, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("first List() error = %v", err)
	}
	if counting.queryCalls != 1 || counting.execCalls != 0 || counting.queryRowCalls != 0 {
		t.Fatalf("list SQL calls query=%d exec=%d queryRow=%d, want one query only", counting.queryCalls, counting.execCalls, counting.queryRowCalls)
	}
	assertPostIDs(t, first.Posts, newestID, sameTimeNewerID)
	if first.NextCursor == nil {
		t.Fatal("first page next cursor = nil")
	}
	if first.Posts[0].CommentCount != 1 || first.Posts[0].LikeCount != 1 || first.Posts[0].LikedByMe {
		t.Fatalf("newest read model = %#v", first.Posts[0])
	}
	if first.Posts[1].CommentCount != 2 || first.Posts[1].LikeCount != 2 || !first.Posts[1].LikedByMe {
		t.Fatalf("same-time newer read model = %#v", first.Posts[1])
	}
	if first.Posts[1].Author.ID != viewerID || first.Posts[1].Author.Username != "PostViewer_"+suffix {
		t.Fatalf("author summary = %#v", first.Posts[1].Author)
	}

	counting.reset()
	wideRecords, err := repository.List(ctx, viewerID, ListOptions{Limit: 50})
	if err != nil {
		t.Fatalf("wide repository List() error = %v", err)
	}
	if len(wideRecords) != 4 || counting.queryCalls != 1 {
		t.Fatalf("wide list records=%d query calls=%d, want 4 records from one query", len(wideRecords), counting.queryCalls)
	}

	insertedBetweenPagesID := insertIntegrationPost(t, ctx, tx, viewerID, "inserted after page one", base.Add(3*time.Minute))
	cursor, err := DecodeCursor(*first.NextCursor)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	counting.reset()
	second, err := service.List(ctx, viewerID, ListOptions{Limit: 2, Cursor: &cursor})
	if err != nil {
		t.Fatalf("second List() error = %v", err)
	}
	if counting.queryCalls != 1 {
		t.Fatalf("second page query calls = %d, want 1", counting.queryCalls)
	}
	assertPostIDs(t, second.Posts, sameTimeOlderID, oldestID)
	if second.NextCursor != nil {
		t.Fatalf("second page next cursor = %q, want nil", *second.NextCursor)
	}
	for _, record := range second.Posts {
		if record.ID == insertedBetweenPagesID || record.ID == newestID || record.ID == sameTimeNewerID {
			t.Fatalf("continuation page repeated or admitted newer record %d", record.ID)
		}
	}

	emptyCursor := Cursor{CreatedAt: base.Add(-time.Microsecond), ID: oldestID}
	empty, err := service.List(ctx, viewerID, ListOptions{Limit: 2, Cursor: &emptyCursor})
	if err != nil || len(empty.Posts) != 0 || empty.NextCursor != nil {
		t.Fatalf("empty page = %#v error=%v", empty, err)
	}

	detail, err := service.Detail(ctx, sameTimeNewerID, viewerID)
	if err != nil || detail.CommentCount != 2 || detail.LikeCount != 2 || !detail.LikedByMe {
		t.Fatalf("Detail() post=%#v error=%v", detail, err)
	}
	if _, err := repository.FindByID(ctx, ^uint64(0), viewerID); err != ErrNotFound {
		t.Fatalf("missing FindByID() error = %v, want ErrNotFound", err)
	}

	query, arguments := listStatement(viewerID, ListOptions{Limit: 2, Cursor: &cursor})
	var plan string
	if err := tx.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+query, arguments...).Scan(&plan); err != nil {
		t.Fatalf("EXPLAIN list query: %v", err)
	}
	for _, expectedIndex := range []string{"idx_posts_created_at_id", "idx_comments_post_id_id", "PRIMARY"} {
		if !strings.Contains(plan, expectedIndex) {
			t.Fatalf("EXPLAIN plan missing %q: %s", expectedIndex, plan)
		}
	}
	t.Logf("validated list EXPLAIN indexes: idx_posts_created_at_id, idx_comments_post_id_id, and PRIMARY")
}

func TestIntegrationPostCreateCommitsOutboxAtomicallyAndRollsBackOnOutboxFailure(t *testing.T) {
	cfg := integrationtest.Environment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	releasePostFactsLock := integrationtest.AcquirePostFactsLock(t, database)
	defer releasePostFactsLock()

	suffix := time.Now().UTC().Format("150405000000")
	result, err := database.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?)`, "PostAtomic_"+suffix, "$2a$10$integration-placeholder")
	if err != nil {
		t.Fatalf("insert atomic test user: %v", err)
	}
	authorID := insertedID(t, result)
	defer func() {
		_, _ = database.ExecContext(context.Background(), `DELETE FROM business_outbox WHERE event_type = 'post.created' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.actor_id')) = ?`, authorID)
		_, _ = database.ExecContext(context.Background(), `DELETE FROM posts WHERE author_id = ?`, authorID)
		_, _ = database.ExecContext(context.Background(), `DELETE FROM users WHERE id = ?`, authorID)
	}()

	eventOutbox, err := outbox.NewRepository(database, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	occurredAt := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	repository := NewMySQLRepository(database, RepositoryOptions{Outbox: eventOutbox, Clock: func() time.Time { return occurredAt }})
	record, err := repository.Create(ctx, authorID, "atomic success "+suffix, "indexed from MySQL")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	var eventType string
	var payload []byte
	if err := database.QueryRowContext(ctx, `SELECT event_type, payload FROM business_outbox WHERE JSON_UNQUOTE(JSON_EXTRACT(payload, '$.post_id')) = ?`, record.ID).Scan(&eventType, &payload); err != nil {
		t.Fatalf("read post.created outbox: %v", err)
	}
	envelope, err := bus.Decode(payload)
	if err != nil || eventType != string(bus.PostCreated) || envelope.PostID != record.ID || envelope.ActorID != authorID || envelope.RecipientID != 0 || envelope.OccurredAt != occurredAt {
		t.Fatalf("post.created outbox event type=%q envelope=%#v error=%v", eventType, envelope, err)
	}

	failingTitle := "atomic rollback " + suffix
	failingRepository := NewMySQLRepository(database, RepositoryOptions{Outbox: failingOutboxWriter{}, Clock: func() time.Time { return occurredAt }})
	if _, err := failingRepository.Create(ctx, authorID, failingTitle, "must roll back"); err == nil {
		t.Fatal("Create() with failing outbox error = nil")
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE author_id = ? AND title = ?`, authorID, failingTitle).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled-back post count=%d error=%v", count, err)
	}
}

type failingOutboxWriter struct{}

func (failingOutboxWriter) Insert(context.Context, outbox.Executor, bus.Envelope) error {
	return errors.New("forced outbox failure")
}

type countingDatabase struct {
	database      database
	execCalls     int
	queryCalls    int
	queryRowCalls int
}

func (counter *countingDatabase) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	counter.execCalls++
	return counter.database.ExecContext(ctx, query, arguments...)
}

func (counter *countingDatabase) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	counter.queryCalls++
	return counter.database.QueryContext(ctx, query, arguments...)
}

func (counter *countingDatabase) QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row {
	counter.queryRowCalls++
	return counter.database.QueryRowContext(ctx, query, arguments...)
}

func (counter *countingDatabase) reset() {
	counter.execCalls = 0
	counter.queryCalls = 0
	counter.queryRowCalls = 0
}

func insertIntegrationUser(t *testing.T, ctx context.Context, tx *sql.Tx, username string) uint64 {
	t.Helper()
	result, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, "$2a$10$integration-placeholder")
	if err != nil {
		t.Fatalf("insert user %q: %v", username, err)
	}
	return insertedID(t, result)
}

func insertIntegrationPost(t *testing.T, ctx context.Context, tx *sql.Tx, authorID uint64, title string, createdAt time.Time) uint64 {
	t.Helper()
	result, err := tx.ExecContext(ctx, `INSERT INTO posts (author_id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, authorID, title, "content: "+title, createdAt, createdAt)
	if err != nil {
		t.Fatalf("insert post %q: %v", title, err)
	}
	return insertedID(t, result)
}

func insertIntegrationComment(t *testing.T, ctx context.Context, tx *sql.Tx, postID, authorID uint64, content string) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `INSERT INTO comments (post_id, author_id, content) VALUES (?, ?, ?)`, postID, authorID, content); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
}

func insertIntegrationLike(t *testing.T, ctx context.Context, tx *sql.Tx, postID, userID uint64) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `INSERT INTO post_likes (post_id, user_id) VALUES (?, ?)`, postID, userID); err != nil {
		t.Fatalf("insert like: %v", err)
	}
}

func insertedID(t *testing.T, result sql.Result) uint64 {
	t.Helper()
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		t.Fatalf("LastInsertId() id=%d error=%v", identifier, err)
	}
	return uint64(identifier)
}

func assertPostIDs(t *testing.T, records []Post, want ...uint64) {
	t.Helper()
	if len(records) != len(want) {
		t.Fatalf("post count=%d, want %d: %#v", len(records), len(want), records)
	}
	for index, identifier := range want {
		if records[index].ID != identifier {
			t.Fatalf("post[%d].ID=%d, want %d", index, records[index].ID, identifier)
		}
	}
}
