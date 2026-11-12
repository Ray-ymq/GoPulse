package post

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/bus"
)

var ErrPermissionDenied = errors.New("post permission denied")

// Update serializes author checks, content revisions and outbox facts on the row.
func (r *MySQLRepository) Update(ctx context.Context, id, actor uint64, input CreateInput) (Post, error) {

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

	record, err := readUpdatedPost(ctx, tx, id, actor)
	if err != nil {
		return Post{}, err
	}
	_, err = r.insertSearchMutation(ctx, tx, event, epoch)
	if err != nil {
		return Post{}, err
	}
	err = tx.Commit()

	return record, err
}

// Read on the same transaction so every response field is known before commit.
func readUpdatedPost(ctx context.Context, tx *sql.Tx, id, viewer uint64) (Post, error) {
	query := fmt.Sprintf(postListReadSelect, "p.created_at", "") + " WHERE p.id=?"
	return scanPost(tx.QueryRowContext(ctx, query, viewer, viewer, viewer, id).Scan)
}
