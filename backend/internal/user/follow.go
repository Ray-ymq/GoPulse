package user

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
)

// SetFollow serializes transitions for one follower; the unique key remains the
// final invariant. The relation and event commit together, including retries.
func (r *MySQLRepository) SetFollow(ctx context.Context, viewer, target uint64, following bool) error {
	if viewer == target {
		return invalid("cannot follow yourself")
	}
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return apperror.WrapInternal(err)
	}
	defer tx.Rollback()
	var id uint64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM users WHERE id = ? FOR UPDATE", viewer).Scan(&id); err != nil {
		return profileError(err)
	}
	if err = tx.QueryRowContext(ctx, "SELECT id FROM users WHERE id = ?", target).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return profileError(ErrNotFound)
	} else if err != nil {
		return profileError(err)
	}
	if following {
		_, err = tx.ExecContext(ctx, "INSERT INTO user_follows (follower_id, followed_id) VALUES (?, ?)", viewer, target)
		if isDuplicateEntry(err) {
			return nil
		}
		if err != nil {
			return profileError(err)
		}
		event, e := bus.NewUserFollowed(time.Now().UTC(), viewer, target)
		if e != nil {
			return profileError(e)
		}
		writer, e := outbox.NewRepository(r.database, outbox.Options{})
		if e != nil {
			return profileError(e)
		}
		if e = writer.Insert(ctx, tx, event); e != nil {
			return profileError(e)
		}
	} else {
		_, err = tx.ExecContext(ctx, "DELETE FROM user_follows WHERE follower_id = ? AND followed_id = ?", viewer, target)
		if err != nil {
			return profileError(err)
		}
	}
	return profileErrorOrNil(tx.Commit())
}
func profileErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return profileError(err)
}
func (r *MySQLRepository) Following(ctx context.Context, viewer, target uint64) (bool, error) {
	var value bool
	err := r.database.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM user_follows WHERE follower_id = ? AND followed_id = ?)", viewer, target).Scan(&value)
	return value, err
}
func (r *MySQLRepository) Relations(ctx context.Context, viewer uint64, followers bool, options RelationOptions) ([]Profile, *string, error) {
	owner, target := "follower_id", "followed_id"
	if followers {
		owner, target = target, owner
	}
	query := `SELECT u.id, u.username, u.display_name, u.bio, u.created_at, f.created_at,
 EXISTS(SELECT 1 FROM user_follows mine WHERE mine.follower_id = ? AND mine.followed_id = u.id)
 FROM user_follows f JOIN users u ON u.id = f.` + target + ` WHERE f.` + owner + ` = ?`
	args := []any{viewer, viewer}
	if options.Cursor != nil {
		query += " AND (f.created_at < ? OR (f.created_at = ? AND u.id < ?))"
		args = append(args, options.Cursor.CreatedAt, options.Cursor.CreatedAt, options.Cursor.ID)
	}
	query += " ORDER BY f.created_at DESC, u.id DESC LIMIT ?"
	args = append(args, options.Limit+1)
	rows, err := r.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, profileError(err)
	}
	defer rows.Close()
	records := []Profile{}
	var last time.Time
	for rows.Next() {
		var p Profile
		var at time.Time
		if err = rows.Scan(&p.ID, &p.Username, &p.DisplayName, &p.Bio, &p.CreatedAt, &at, &p.Following); err != nil {
			return nil, nil, profileError(err)
		}
		p.IsSelf = p.ID == viewer
		records = append(records, p)
		if len(records) <= options.Limit {
			last = at
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, profileError(err)
	}
	if len(records) > options.Limit {
		records = records[:options.Limit]
		token, err := encodeRelationCursor(last, records[len(records)-1].ID)
		return records, &token, profileErrorOrNil(err)
	}
	return records, nil, nil
}

type RelationOptions struct {
	Limit  int
	Cursor *RelationCursor
}
type RelationCursor struct {
	CreatedAt time.Time
	ID        uint64
}

func encodeRelationCursor(at time.Time, id uint64) (string, error) {
	payload, err := json.Marshal(struct {
		Version   int    `json:"v"`
		CreatedAt string `json:"created_at"`
		ID        uint64 `json:"id"`
	}{1, at.UTC().Format(time.RFC3339Nano), id})
	return base64.RawURLEncoding.EncodeToString(payload), err
}
