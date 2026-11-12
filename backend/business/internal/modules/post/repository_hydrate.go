package post

import (
	"context"
	"strings"
)

// HydrateAuthors refreshes mutable summaries after cache reads, in one query.
func (repository *MySQLRepository) HydrateAuthors(ctx context.Context, records []Post) (err error) {

	if len(records) == 0 {
		return nil
	}
	ids := make([]any, 0, len(records))
	slots := make([]string, 0, len(records))
	for _, p := range records {
		ids = append(ids, p.Author.ID)
		slots = append(slots, "?")
	}
	rows, err := repository.database.QueryContext(ctx, "SELECT id, username, display_name FROM users WHERE id IN ("+strings.Join(slots, ",")+")", ids...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	authors := map[uint64]Author{}
	for rows.Next() {
		var a Author
		if err = rows.Scan(&a.ID, &a.Username, &a.DisplayName); err != nil {
			return err
		}
		authors[a.ID] = a
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range records {
		a, ok := authors[records[i].Author.ID]
		if !ok {
			return ErrNotFound
		}
		records[i].Author = a
	}
	return nil
}

func (r *MySQLRepository) HydrateFollowing(ctx context.Context, records []Post, viewer uint64) (err error) {

	if len(records) == 0 || viewer == 0 {
		return nil
	}
	args := []any{viewer}
	slots := []string{}
	for _, p := range records {
		args = append(args, p.Author.ID)
		slots = append(slots, "?")
	}
	rows, err := r.database.QueryContext(ctx, "SELECT followed_id FROM user_follows WHERE follower_id = ? AND followed_id IN ("+strings.Join(slots, ",")+")", args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	followed := map[uint64]bool{}
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			return err
		}
		followed[id] = true
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range records {
		records[i].Author.Following = followed[records[i].Author.ID]
	}
	return nil
}

// HydrateBookmarks never writes personalized facts into a shared projection.
func (r *MySQLRepository) HydrateBookmarks(ctx context.Context, records []Post, viewer uint64) (err error) {

	for i := range records {
		records[i].BookmarkedByMe = false
	}
	if viewer == 0 || len(records) == 0 {
		return nil
	}
	args := []any{viewer}
	slots := []string{}
	for _, p := range records {
		args = append(args, p.ID)
		slots = append(slots, "?")
	}
	rows, err := r.database.QueryContext(ctx, "SELECT post_id FROM post_bookmarks WHERE user_id = ? AND post_id IN ("+strings.Join(slots, ",")+")", args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	found := map[uint64]bool{}
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		found[id] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range records {
		records[i].BookmarkedByMe = found[records[i].ID]
	}
	return nil
}
