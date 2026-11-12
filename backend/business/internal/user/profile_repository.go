package user

import (
	"context"
	"strings"
)

func (r *MySQLRepository) UpdateProfile(ctx context.Context, id uint64, name, bio string) (User, error) {
	_, err := r.database.ExecContext(ctx, "UPDATE users SET display_name = ?, bio = ? WHERE id = ?", name, bio, id)
	if err != nil {
		return User{}, err
	}
	return r.FindByID(ctx, id)
}
func (r *MySQLRepository) SearchProfiles(ctx context.Context, viewer uint64, options UserSearchOptions) ([]Profile, []int, error) {
	// Explicit escape character makes %, _ and ! literal independently of SQL mode.
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(options.Query)
	query := `SELECT id, username, display_name, bio, created_at, match_rank, EXISTS(SELECT 1 FROM user_follows f WHERE f.follower_id = ? AND f.followed_id = matches.id) FROM (
 SELECT id, username, display_name, bio, created_at,
 CASE WHEN username = ? THEN 0 WHEN username LIKE ? ESCAPE '!' OR display_name LIKE ? ESCAPE '!' THEN 1 ELSE 2 END AS match_rank
 FROM users WHERE username LIKE ? ESCAPE '!' OR display_name LIKE ? ESCAPE '!'
 ) AS matches`
	args := []any{viewer, options.Query, escaped + "%", escaped + "%", "%" + escaped + "%", "%" + escaped + "%"}
	if options.after != nil {
		query += " WHERE match_rank > ? OR (match_rank = ? AND id > ?)"
		args = append(args, options.after.Rank, options.after.Rank, options.after.ID)
	}
	query += " ORDER BY match_rank ASC, id ASC LIMIT ?"
	args = append(args, options.Limit+1)
	rows, err := r.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	records := []Profile{}
	ranks := []int{}
	for rows.Next() {
		var p Profile
		var rank int
		if err = rows.Scan(&p.ID, &p.Username, &p.DisplayName, &p.Bio, &p.CreatedAt, &rank, &p.Following); err != nil {
			return nil, nil, err
		}
		p.IsSelf = p.ID == viewer
		records = append(records, p)
		ranks = append(ranks, rank)
	}
	return records, ranks, rows.Err()
}
