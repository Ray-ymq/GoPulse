package post

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/tracing"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"time"
)

var ErrPermissionDenied = errors.New("post permission denied")

// Update serializes author checks, content revisions and outbox facts on the row.
func (r *MySQLRepository) Update(ctx context.Context, id, actor uint64, input CreateInput) error {
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
		if metrics := componentmetrics.BackendActive(); metrics != nil {
			metrics.ObserveFreshness("commit", result, time.Since(commitStarted))
		}
		if !committed {
			commitSpan.SetStatus(codes.Error, "commit_failed")
		}
	}()
	starter, ok := r.database.(transactionStarter)
	if !ok || r.outbox == nil {
		return errors.New("post edit requires transactional outbox")
	}
	tx, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner, revision uint64
	var title, content string
	err = tx.QueryRowContext(ctx, `SELECT author_id, title, content, content_revision FROM posts WHERE id = ? FOR UPDATE`, id).Scan(&owner, &title, &content, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != actor {
		return ErrPermissionDenied
	}
	if title == input.Title && content == input.Content {
		err := tx.Commit()
		committed = err == nil
		return err
	}
	at := r.clock().UTC()
	revision++
	if _, err = tx.ExecContext(ctx, `UPDATE posts SET title=?, content=?, edited_at=?, updated_at=?, content_revision=? WHERE id=?`, input.Title, input.Content, at, at, revision, id); err != nil {
		return err
	}
	event, err := bus.NewPostUpdated(at, actor, id, revision)
	if err != nil {
		return err
	}
	commitSpan.SetAttributes(
		attribute.String("gopulse.event_id", event.EventID),
		attribute.Int64("gopulse.content_revision", int64(revision)),
	)
	outboxID, err := insertOutbox(ctx, tx, r.outbox, event)
	if err != nil {
		return err
	}
	if outboxID > 0 {
		commitSpan.SetAttributes(attribute.Int64("gopulse.outbox_id", int64(outboxID)))
	}
	err = tx.Commit()
	committed = err == nil
	return err
}

func (r *MySQLRepository) ContentRevision(ctx context.Context, id uint64) (uint64, error) {
	var revision uint64
	err := r.database.QueryRowContext(ctx, `SELECT content_revision FROM posts WHERE id=?`, id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return revision, err
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
		Update(context.Context, uint64, uint64, CreateInput) error
	})
	if !ok {
		return Post{}, apperror.WrapInternal(errors.New("post editing unavailable"))
	}
	err = r.Update(ctx, id, actor, normalized)
	if errors.Is(err, ErrNotFound) {
		return Post{}, apperror.New(apperror.CodePostNotFound, "post not found")
	}
	if errors.Is(err, ErrPermissionDenied) {
		return Post{}, apperror.New(apperror.CodePermissionDenied, "only the author may edit this post")
	}
	if err != nil {
		return Post{}, apperror.WrapInternal(err)
	}
	if s.cache != nil {
		_ = s.cache.Invalidate(ctx, id)
	}
	return s.Detail(ctx, id, actor)
}
