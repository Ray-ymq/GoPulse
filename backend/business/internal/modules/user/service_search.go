package user

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/backend/internal/shared/apperror"
)

type userSearchCursor struct {
	Version int    `json:"v"`
	Query   string `json:"q"`
	Rank    int    `json:"rank"`
	ID      uint64 `json:"id"`
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
