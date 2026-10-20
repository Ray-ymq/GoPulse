package post

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/instrumentation"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"time"
)

var ErrPermissionDenied = errors.New("post permission denied")

// Update serializes author checks, content revisions and outbox facts on the row.
func (r *MySQLRepository) Update(ctx context.Context, id, actor uint64, input CreateInput) (Post, error) {
	commitContext, commitSpan := tracing.Start(ctx, "post.commit", attribute.String("gopulse.operation", "update"), attribute.Int64("gopulse.post_id", int64(id)))
	defer commitSpan.End()
	ctx = commitContext
	commitStarted := time.Now()
	committed := false
	defer func() {
		result := "failure"
		if committed {
			result = "success"
		}
		if metrics := instrumentation.BackendActive(); metrics != nil {
			metrics.ObserveFreshness("commit", result, time.Since(commitStarted))
		}
		if !committed {
			commitSpan.SetStatus(codes.Error, "commit_failed")
		}
	}()
	starter, ok := r.database.(transactionStarter)
	if !ok || r.outbox == nil {
		return Post{}, errors.New("post edit requires transactional outbox")
	}
	tx, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Post{}, err
	}
	defer func() { _ = tx.Rollback() }()
	epoch, err := r.mutationEpoch(ctx, tx)
	if err != nil {
		return Post{}, err
	}
	var owner, revision uint64
	var title, content string
	err = tx.QueryRowContext(ctx, `SELECT author_id, title, content, content_revision FROM posts WHERE id = ? FOR UPDATE`, id).Scan(&owner, &title, &content, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, err
	}
	if owner != actor {
		return Post{}, ErrPermissionDenied
	}
	if title == input.Title && content == input.Content {
		record, err := readUpdatedPost(ctx, tx, id, actor)
		if err != nil {
			return Post{}, err
		}
		err = tx.Commit()
		committed = err == nil
		return record, err
	}
	at := r.clock().UTC()
	revision++
	if _, err = tx.ExecContext(ctx, `UPDATE posts SET title=?, content=?, edited_at=?, updated_at=?, content_revision=? WHERE id=?`, input.Title, input.Content, at, at, revision, id); err != nil {
		return Post{}, err
	}
	event, err := bus.NewPostUpdated(at, actor, id, revision)
	if err != nil {
		return Post{}, err
	}
	commitSpan.SetAttributes(
		attribute.String("gopulse.event_id", event.EventID),
		attribute.Int64("gopulse.content_revision", int64(revision)),
	)
	outboxID, err := r.insertSearchMutation(ctx, tx, event, epoch)
	if err != nil {
		return Post{}, err
	}
	if outboxID > 0 {
		commitSpan.SetAttributes(attribute.Int64("gopulse.outbox_id", int64(outboxID)))
	}
	record, err := readUpdatedPost(ctx, tx, id, actor)
	if err != nil {
		return Post{}, err
	}
	err = tx.Commit()
	committed = err == nil
	return record, err
}

// Read on the same transaction so every response field is known before commit.
func readUpdatedPost(ctx context.Context, tx *sql.Tx, id, viewer uint64) (Post, error) {
	query := fmt.Sprintf(postListReadSelect, "p.created_at", "") + " WHERE p.id=?"
	return scanPost(tx.QueryRowContext(ctx, query, viewer, viewer, viewer, id).Scan)
}

func (s *Service) Update(ctx context.Context, id, actor uint64, input CreateInput) (Post, error) {
	normalized, err := NormalizeCreateInput(input)
	if errors.Is(err, ErrInvalidTitle) {
		return Post{}, validationError("title must contain between 1 and 120 characters")
	}
	if errors.Is(err, ErrInvalidContent) {
		return Post{}, validationError("content must contain between 1 and 10000 characters")
	}
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}
	r, ok := s.repository.(interface {
		Update(context.Context, uint64, uint64, CreateInput) (Post, error)
	})
	if !ok {
		return Post{}, apperror.WrapInternal(errors.New("post editing unavailable"))
	}
	record, err := r.Update(ctx, id, actor, normalized)
	if errors.Is(err, ErrNotFound) {
		return Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
	}
	if errors.Is(err, ErrPermissionDenied) {
		return Post{}, apperror.New(apperror.CodePermissionDenied, "only the author may edit this post")
	}
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}
	return record, nil
}
