package post

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/shared/correlation"
)

func (repository *MySQLRepository) Create(ctx context.Context, authorID uint64, title, content string) (Post, error) {
	if repository.state != nil && repository.outbox != nil {
		result, err := repository.CreateWithCommand(ctx, authorID, title, content, CommandRequest{}, digestCreateInput(CreateInput{Title: title, Content: content}))
		if err != nil {
			return Post{}, err
		}
		if result.Post == nil {
			return Post{}, errors.New("create post: committed result has no post")
		}
		return *result.Post, nil
	}
	if repository.outbox == nil {
		record, err := createPost(ctx, repository.database, authorID, title, content)
		if err == nil {
			repository.invalidateDiscovery(ctx, "")
		}
		return record, err
	}
	commitAttemptID := correlation.NewAttemptID()
	commitContext := ctx
	commitContext = correlation.WithAttemptID(commitContext, commitAttemptID)

	ctx = commitContext

	starter, ok := repository.database.(transactionStarter)
	if !ok {
		return Post{}, errors.New("create post: database does not support transactions")
	}
	transaction, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Post{}, errors.New("begin post transaction")
	}
	defer func() { _ = transaction.Rollback() }()
	record, err := createPost(ctx, transaction, authorID, title, content)
	if err != nil {
		return Post{}, err
	}
	event, err := bus.NewPostCreated(repository.clock().UTC(), authorID, record.ID)
	if err != nil {
		return Post{}, errors.New("create post event")
	}
	event.ContentRevision = record.ContentRevision

	outboxID, err := insertOutbox(ctx, transaction, repository.outbox, event)
	if err != nil {
		return Post{}, errors.New("create post outbox event")
	}
	if err := transaction.Commit(); err != nil {
		return Post{}, errors.New("commit post transaction")
	}
	repository.invalidateDiscovery(ctx, "")
	repository.logger.Info("post committed",
		slog.String("event_id", event.EventID),
		slog.Uint64("post_id", record.ID),
		slog.Uint64("content_revision", record.ContentRevision),
		slog.Uint64("outbox_id", outboxID),
		slog.String("attempt_id", commitAttemptID),
		slog.String("stage", "commit"),
		slog.String("result", "success"),
	)
	return record, nil
}

func createPost(ctx context.Context, database database, authorID uint64, title, content string) (Post, error) {
	result, err := database.ExecContext(ctx, `INSERT INTO posts (author_id, title, content) VALUES (?, ?, ?)`, authorID, title, content)
	if err != nil {
		return Post{}, fmt.Errorf("create post: %w", err)
	}
	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		return Post{}, errors.New("create post: invalid inserted identifier")
	}
	projection, err := findPublicByID(ctx, database, uint64(identifier))
	if err != nil {
		return Post{}, err
	}
	return projection.post(false), nil
}
