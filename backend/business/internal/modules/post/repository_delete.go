package post

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/bus"
)

// Delete serializes with edits and atomically removes facts and publishes intent.
func (r *MySQLRepository) Delete(ctx context.Context, id, actor uint64) error {

	starter, ok := r.database.(transactionStarter)
	if !ok || r.outbox == nil {
		return errors.New("post deletion requires transactional outbox")
	}
	tx, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	epoch, err := r.mutationEpoch(ctx, tx)
	if err != nil {
		return err
	}
	var owner, revision uint64
	err = tx.QueryRowContext(ctx, "SELECT author_id, content_revision FROM posts WHERE id=? FOR UPDATE", id).Scan(&owner, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != actor {
		return ErrPermissionDenied
	}
	deletionEpoch := epoch
	if deletionEpoch == "" {
		if err := tx.QueryRowContext(ctx, "SELECT data_epoch FROM business_runtime_state WHERE singleton=1 FOR SHARE").Scan(&deletionEpoch); err != nil {
			return err
		}
	}
	revision++
	if _, err := tx.ExecContext(ctx, `INSERT INTO post_search_tombstones(data_epoch,post_id,content_revision,deleted_at) VALUES (?,?,?,?)`, deletionEpoch, id, revision, r.clock().UTC()); err != nil {
		return err
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
	event.ContentRevision = revision

	_, err = r.insertSearchMutation(ctx, tx, event, epoch)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.invalidateCounts(ctx, deletionEpoch, id)
	r.invalidateDiscovery(ctx, deletionEpoch)
	return nil
}
