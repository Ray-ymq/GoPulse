package user

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

// Profile deliberately excludes authentication and authorization fields.
type Profile struct {
	ID          uint64    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Bio         string    `json:"bio"`
	CreatedAt   time.Time `json:"created_at"`
	Following   bool      `json:"following"`
	IsSelf      bool      `json:"is_self"`
}
type ProfileInput struct {
	DisplayName *string `json:"display_name"`
	Bio         *string `json:"bio"`
}

func NormalizeProfile(input ProfileInput) (string, string, error) {
	if input.DisplayName == nil || input.Bio == nil {
		return "", "", invalid("display_name and bio are required")
	}
	name, bio := strings.TrimSpace(*input.DisplayName), strings.TrimSpace(*input.Bio)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 {
		return "", "", invalid("display_name must contain 1–64 characters")
	}
	if !utf8.ValidString(bio) || utf8.RuneCountInString(bio) > 160 {
		return "", "", invalid("bio must contain 0–160 characters")
	}
	return name, bio, nil
}
func invalid(message string) error { return apperror.New(apperror.CodeValidationFailed, message) }
func profile(record User, viewer uint64) Profile {
	return Profile{record.ID, record.Username, record.DisplayName, record.Bio, record.CreatedAt, false, record.ID == viewer}
}
func profileError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return apperror.New(apperror.CodeUserNotFound, "user not found")
	}
	return apperror.WrapInternal(err)
}

type ProfileRepository interface {
	FindByID(context.Context, uint64) (User, error)
	FindByUsername(context.Context, string) (User, error)
	UpdateProfile(context.Context, uint64, string, string) (User, error)
	SearchProfiles(context.Context, uint64, UserSearchOptions) ([]Profile, []int, error)
}
type ProfileService struct {
	repository ProfileRepository
	cursorKey  []byte
}

func NewProfileService(repository ProfileRepository, secret string) *ProfileService {
	key := sha256.Sum256([]byte("gopulse/user-search/v1\x00" + secret))
	return &ProfileService{repository: repository, cursorKey: key[:]}
}
func (s *ProfileService) Get(ctx context.Context, username string, viewer uint64) (Profile, error) {
	record, err := s.repository.FindByUsername(ctx, username)
	if err != nil {
		return Profile{}, profileError(err)
	}
	p := profile(record, viewer)
	if r, ok := s.repository.(interface {
		Following(context.Context, uint64, uint64) (bool, error)
	}); ok {
		p.Following, err = r.Following(ctx, viewer, record.ID)
		if err != nil {
			return Profile{}, profileError(err)
		}
	}
	return p, nil
}
func (s *ProfileService) Update(ctx context.Context, viewer uint64, input ProfileInput) (Profile, error) {
	name, bio, err := NormalizeProfile(input)
	if err != nil {
		return Profile{}, err
	}
	record, err := s.repository.UpdateProfile(ctx, viewer, name, bio)
	if err != nil {
		return Profile{}, profileError(err)
	}
	p := profile(record, viewer)
	if r, ok := s.repository.(interface {
		Following(context.Context, uint64, uint64) (bool, error)
	}); ok {
		p.Following, err = r.Following(ctx, viewer, record.ID)
		if err != nil {
			return Profile{}, profileError(err)
		}
	}
	return p, nil
}

type userSearchCursor struct {
	Version int    `json:"v"`
	Query   string `json:"q"`
	Rank    int    `json:"rank"`
	ID      uint64 `json:"id"`
}
type UserSearchOptions struct {
	Query string
	Limit int
	after *userSearchCursor
}

func (s *ProfileService) ParseSearch(values url.Values) (UserSearchOptions, error) {
	options := UserSearchOptions{Limit: 20}
	for k, v := range values {
		if (k != "q" && k != "limit" && k != "cursor") || len(v) != 1 {
			return options, invalid("invalid search parameters")
		}
	}
	options.Query = strings.TrimSpace(values.Get("q"))
	if !utf8.ValidString(options.Query) || utf8.RuneCountInString(options.Query) < 1 || utf8.RuneCountInString(options.Query) > 200 {
		return options, invalid("q must contain 1–200 characters")
	}
	if raw, ok := values["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 50 || strconv.Itoa(n) != raw[0] {
			return options, invalid("limit must be between 1 and 50")
		}
		options.Limit = n
	}
	if raw, ok := values["cursor"]; ok {
		token := raw[0]
		if len(token) > 2048 {
			return options, invalid("invalid cursor")
		}
		bytes, err := base64.RawURLEncoding.Strict().DecodeString(token)
		if err != nil || len(bytes) <= sha256.Size {
			return options, invalid("invalid cursor")
		}
		payload, sig := bytes[:len(bytes)-sha256.Size], bytes[len(bytes)-sha256.Size:]
		mac := hmac.New(sha256.New, s.cursorKey)
		mac.Write(payload)
		var cursor userSearchCursor
		if !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(payload, &cursor) != nil || cursor.Version != 1 || cursor.Query != options.Query || cursor.ID == 0 || cursor.Rank < 0 || cursor.Rank > 2 {
			return options, invalid("invalid cursor")
		}
		options.after = &cursor
	}
	return options, nil
}
func (s *ProfileService) Search(ctx context.Context, viewer uint64, options UserSearchOptions) ([]Profile, *string, error) {
	records, ranks, err := s.repository.SearchProfiles(ctx, viewer, options)
	if err != nil {
		return nil, nil, apperror.WrapInternal(err)
	}
	if len(records) <= options.Limit {
		return records, nil, nil
	}
	records = records[:options.Limit]
	last := len(records) - 1
	payload, err := json.Marshal(userSearchCursor{1, options.Query, ranks[last], records[last].ID})
	if err != nil {
		return nil, nil, apperror.WrapInternal(err)
	}
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write(payload)
	token := base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
	return records, &token, nil
}
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
	defer rows.Close()
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
