//go:build integration

package comment

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

func TestIntegrationCommentRepositoryServicePaginationAndForeignKeys(t *testing.T) {
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
	postAuthorID := insertCommentIntegrationUser(t, ctx, tx, "CommentPost_"+suffix)
	commenterID := insertCommentIntegrationUser(t, ctx, tx, "Commenter_"+suffix)
	postID := insertCommentIntegrationPost(t, ctx, tx, postAuthorID)
	postService := post.NewService(post.NewMySQLRepository(tx))
	counting := &countingCommentDatabase{database: tx}
	repository := NewMySQLRepository(counting)
	service := NewService(repository, postService)

	first, err := service.Create(ctx, postID, postAuthorID, CreateInput{Content: "  first comment  "})
	if err != nil || first.Content != "first comment" || first.Author.ID != postAuthorID || first.Author.Username != "CommentPost_"+suffix {
		t.Fatalf("first Create() comment=%#v error=%v", first, err)
	}
	second, err := service.Create(ctx, postID, commenterID, CreateInput{Content: "second comment"})
	if err != nil || second.Author.ID != commenterID || second.Author.Username != "Commenter_"+suffix {
		t.Fatalf("second Create() comment=%#v error=%v", second, err)
	}
	third, err := service.Create(ctx, postID, postAuthorID, CreateInput{Content: "third comment"})
	if err != nil {
		t.Fatalf("third Create() error=%v", err)
	}

	counting.reset()
	pageOne, err := service.List(ctx, postID, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("first List() error=%v", err)
	}
	if counting.queryCalls != 1 || counting.execCalls != 0 || counting.queryRowCalls != 0 {
		t.Fatalf("comment list SQL calls query=%d exec=%d queryRow=%d, want one list query", counting.queryCalls, counting.execCalls, counting.queryRowCalls)
	}
	assertCommentIDs(t, pageOne.Comments, third.ID, second.ID)
	if pageOne.NextCursor == nil {
		t.Fatal("first page next cursor=nil")
	}

	insertedAfterPage, err := service.Create(ctx, postID, commenterID, CreateInput{Content: "inserted after first page"})
	if err != nil {
		t.Fatalf("insert between pages: %v", err)
	}
	cursor, err := DecodeCursor(*pageOne.NextCursor)
	if err != nil {
		t.Fatalf("DecodeCursor() error=%v", err)
	}
	counting.reset()
	pageTwo, err := service.List(ctx, postID, ListOptions{Limit: 2, Cursor: &cursor})
	if err != nil {
		t.Fatalf("second List() error=%v", err)
	}
	if counting.queryCalls != 1 {
		t.Fatalf("second page query calls=%d, want 1", counting.queryCalls)
	}
	assertCommentIDs(t, pageTwo.Comments, first.ID)
	if pageTwo.NextCursor != nil {
		t.Fatalf("second page next cursor=%q, want nil", *pageTwo.NextCursor)
	}
	for _, record := range pageTwo.Comments {
		if record.ID == insertedAfterPage.ID || record.ID == third.ID || record.ID == second.ID {
			t.Fatalf("continuation page repeated or admitted newer comment %d", record.ID)
		}
	}

	detail, err := postService.Detail(ctx, postID, postAuthorID)
	if err != nil || detail.CommentCount != 4 {
		t.Fatalf("Detail() post=%#v error=%v", detail, err)
	}

	query, arguments := listStatement(postID, ListOptions{Limit: 2, Cursor: &cursor})
	var plan string
	if err := tx.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+query, arguments...).Scan(&plan); err != nil {
		t.Fatalf("EXPLAIN comment list: %v", err)
	}
	for _, expectedIndex := range []string{"idx_comments_post_id_id", "PRIMARY"} {
		if !strings.Contains(plan, expectedIndex) {
			t.Fatalf("EXPLAIN plan missing %q: %s", expectedIndex, plan)
		}
	}
	t.Logf("validated comment list EXPLAIN indexes: idx_comments_post_id_id and PRIMARY")

	if _, err := repository.Create(ctx, postID, ^uint64(0), "invalid author"); err == nil {
		t.Fatal("Create() with missing author succeeded; want foreign-key failure")
	}
	if _, err := repository.Create(ctx, ^uint64(0), commenterID, "invalid post"); err == nil {
		t.Fatal("Create() with missing post succeeded; want foreign-key failure")
	}
	_, err = service.Create(ctx, ^uint64(0), commenterID, CreateInput{Content: "missing post"})
	assertCommentCode(t, err, apperror.CodePostNotFound)
	_, err = service.List(ctx, ^uint64(0), ListOptions{Limit: 20})
	assertCommentCode(t, err, apperror.CodePostNotFound)
}

type countingCommentDatabase struct {
	database      database
	execCalls     int
	queryCalls    int
	queryRowCalls int
}

func (counter *countingCommentDatabase) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	counter.execCalls++
	return counter.database.ExecContext(ctx, query, arguments...)
}

func (counter *countingCommentDatabase) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	counter.queryCalls++
	return counter.database.QueryContext(ctx, query, arguments...)
}

func (counter *countingCommentDatabase) QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row {
	counter.queryRowCalls++
	return counter.database.QueryRowContext(ctx, query, arguments...)
}

func (counter *countingCommentDatabase) reset() {
	counter.execCalls = 0
	counter.queryCalls = 0
	counter.queryRowCalls = 0
}

func insertCommentIntegrationUser(t *testing.T, ctx context.Context, tx *sql.Tx, username string) uint64 {
	t.Helper()
	result, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, "$2a$10$integration-placeholder")
	if err != nil {
		t.Fatalf("insert user %q: %v", username, err)
	}
	return commentInsertedID(t, result)
}

func insertCommentIntegrationPost(t *testing.T, ctx context.Context, tx *sql.Tx, authorID uint64) uint64 {
	t.Helper()
	result, err := tx.ExecContext(ctx, `INSERT INTO posts (author_id, title, content) VALUES (?, ?, ?)`, authorID, "comment integration", "content")
	if err != nil {
		t.Fatalf("insert post: %v", err)
	}
	return commentInsertedID(t, result)
}

func commentInsertedID(t *testing.T, result sql.Result) uint64 {
	t.Helper()
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		t.Fatalf("LastInsertId() id=%d error=%v", identifier, err)
	}
	return uint64(identifier)
}

func assertCommentIDs(t *testing.T, records []Comment, want ...uint64) {
	t.Helper()
	if len(records) != len(want) {
		t.Fatalf("comment count=%d, want %d: %#v", len(records), len(want), records)
	}
	for index, identifier := range want {
		if records[index].ID != identifier {
			t.Fatalf("comment[%d].ID=%d, want %d", index, records[index].ID, identifier)
		}
	}
}

func assertCommentCode(t *testing.T, err error, code apperror.Code) {
	t.Helper()
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != code {
		t.Fatalf("error=%#v, want code %q", err, code)
	}
}
