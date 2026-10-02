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

// Delete serializes with edits and atomically removes facts and publishes intent.
func (r *MySQLRepository) Delete(ctx context.Context, id, actor uint64) error {
	commitContext, commitSpan := tracing.Start(ctx, "post.commit", attribute.String("gopulse.operation", "delete"), attribute.Int64("gopulse.post_id", int64(id)))
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
		return errors.New("post deletion requires transactional outbox")
	}
	// Reindex holds this same lock across snapshot copy and alias switching.
	// Serialize deletion with rebuilds so an old bulk snapshot cannot resurrect it.
	if db, ok := r.database.(*sql.DB); ok {
		conn, err := db.Conn(ctx)
		if err != nil {
			return err
		}
		defer conn.Close()
		var acquired int
		if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK('gopulse:post-search-reindex:v1', 10)").Scan(&acquired); err != nil {
			return err
		}
		if acquired != 1 {
			return errors.New("search rebuild busy; retry deletion")
		}
		defer conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK('gopulse:post-search-reindex:v1')")
		starter = conn
	}
	tx, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner uint64
	err = tx.QueryRowContext(ctx, "SELECT author_id FROM posts WHERE id=? FOR UPDATE", id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != actor {
		return ErrPermissionDenied
	}
	for _, query := range []string{"UPDATE notifications SET post_id=NULL, comment_id=NULL WHERE post_id=?", "DELETE FROM post_bookmarks WHERE post_id=?", "DELETE FROM post_likes WHERE post_id=?", "DELETE FROM comments WHERE post_id=?", "DELETE FROM posts WHERE id=?"} {
		if _, err = tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	event, err := bus.NewPostDeleted(r.clock().UTC(), actor, id)
	if err != nil {
		return err
	}
	commitSpan.SetAttributes(attribute.String("gopulse.event_id", event.EventID))
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

func (s *Service) Delete(ctx context.Context, id, actor uint64) error {
	r, ok := s.repository.(interface {
		Delete(context.Context, uint64, uint64) error
	})
	if !ok {
		return apperror.WrapInternal(errors.New("post deletion unavailable"))
	}
	err := r.Delete(ctx, id, actor)
	if errors.Is(err, ErrNotFound) {
		return apperror.New(apperror.CodePostNotFound, "post not found")
	}
	if errors.Is(err, ErrPermissionDenied) {
		return apperror.New(apperror.CodePermissionDenied, "only the author may delete this post")
	}
	if err != nil {
		return apperror.WrapInternal(err)
	}
	if s.cache != nil {
		_ = s.cache.Invalidate(ctx, id)
	}
	return nil
}
