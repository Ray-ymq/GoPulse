package search

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

const (
	DefaultLimit  = 20
	MaximumLimit  = 50
	cursorVersion = 1
)

type Options struct {
	Query  string
	Limit  int
	Cursor *Cursor
}

type Cursor struct {
	QueryDigest string
	Generation  string
	After       Hit
}

type cursorPayload struct {
	Version     int     `json:"v"`
	QueryDigest string  `json:"q"`
	Generation  string  `json:"generation"`
	Score       float64 `json:"score"`
	CreatedAt   string  `json:"created_at"`
	PostID      uint64  `json:"post_id"`
}

type Searcher interface {
	ResolveGeneration(context.Context) (string, error)
	Search(context.Context, string, string, int, *Hit) (SearchResult, error)
}

type Hydrator interface {
	FindMany(context.Context, uint64, []uint64) ([]post.Post, error)
}

type Page struct {
	Posts      []post.Post
	NextCursor *string
}

type Service struct {
	searcher Searcher
	hydrator Hydrator
}

func NewService(searcher Searcher, hydrator Hydrator) *Service {
	return &Service{searcher: searcher, hydrator: hydrator}
}

func ParseOptions(values url.Values) (Options, error) {
	for key := range values {
		if key != "q" && key != "limit" && key != "cursor" {
			return Options{}, validationError("search parameters are invalid")
		}
	}
	queries, ok := values["q"]
	if !ok || len(queries) != 1 || !utf8.ValidString(queries[0]) {
		return Options{}, validationError("q must contain between 1 and 200 characters")
	}
	query := strings.TrimSpace(queries[0])
	if length := utf8.RuneCountInString(query); length < 1 || length > 200 {
		return Options{}, validationError("q must contain between 1 and 200 characters")
	}
	options := Options{Query: query, Limit: DefaultLimit}
	if limits, ok := values["limit"]; ok {
		if len(limits) != 1 || limits[0] == "" {
			return Options{}, validationError("limit must be an integer between 1 and 50")
		}
		for _, character := range limits[0] {
			if character < '0' || character > '9' {
				return Options{}, validationError("limit must be an integer between 1 and 50")
			}
		}
		limit, err := strconv.ParseUint(limits[0], 10, 8)
		if err != nil || limit < 1 || limit > MaximumLimit {
			return Options{}, validationError("limit must be an integer between 1 and 50")
		}
		options.Limit = int(limit)
	}
	if cursors, ok := values["cursor"]; ok {
		if len(cursors) != 1 {
			return Options{}, validationError("cursor is invalid")
		}
		cursor, err := DecodeCursor(cursors[0])
		if err != nil || cursor.QueryDigest != digestQuery(query) {
			return Options{}, validationError("cursor is invalid")
		}
		options.Cursor = &cursor
	}
	return options, nil
}

func (service *Service) Search(ctx context.Context, viewerID uint64, options Options) (Page, error) {
	if service == nil || service.searcher == nil || service.hydrator == nil || viewerID == 0 {
		return Page{}, apperror.WrapInternal(errors.New("search service is unavailable"))
	}
	generation, err := service.searcher.ResolveGeneration(ctx)
	if err != nil {
		return Page{}, unavailableError()
	}
	var after *Hit
	if options.Cursor != nil {
		if options.Cursor.Generation != generation || options.Cursor.QueryDigest != digestQuery(options.Query) {
			return Page{}, validationError("cursor is invalid")
		}
		after = &options.Cursor.After
	}
	result, err := service.searcher.Search(ctx, generation, options.Query, options.Limit, after)
	if err != nil || result.Generation != generation {
		return Page{}, unavailableError()
	}
	visibleHits := result.Hits
	hasMore := len(visibleHits) > options.Limit
	if hasMore {
		visibleHits = visibleHits[:options.Limit]
	}
	identifiers := make([]uint64, len(visibleHits))
	for index, hit := range visibleHits {
		identifiers[index] = hit.PostID
	}
	records := make([]post.Post, 0)
	if len(identifiers) > 0 {
		records, err = service.hydrator.FindMany(ctx, viewerID, identifiers)
		if err != nil || len(records) != len(identifiers) {
			return Page{}, apperror.WrapInternal(errors.New("hydrate search results"))
		}
	}
	page := Page{Posts: records}
	if hasMore {
		last := visibleHits[len(visibleHits)-1]
		token, err := EncodeCursor(Cursor{QueryDigest: digestQuery(options.Query), Generation: generation, After: last})
		if err != nil {
			return Page{}, apperror.WrapInternal(err)
		}
		page.NextCursor = &token
	}
	return page, nil
}

func EncodeCursor(cursor Cursor) (string, error) {
	if cursor.QueryDigest == "" || !validPhysicalIndex(cursor.Generation) || cursor.After.PostID == 0 || cursor.After.CreatedAt == "" || cursor.After.Score < 0 {
		return "", errors.New("search cursor fields are invalid")
	}
	payload, err := json.Marshal(cursorPayload{
		Version: cursorVersion, QueryDigest: cursor.QueryDigest, Generation: cursor.Generation,
		Score: cursor.After.Score, CreatedAt: cursor.After.CreatedAt, PostID: cursor.After.PostID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeCursor(token string) (Cursor, error) {
	if token == "" || len(token) > 2048 {
		return Cursor{}, errors.New("cursor is invalid")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return Cursor{}, errors.New("cursor is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var payload cursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return Cursor{}, errors.New("cursor is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Cursor{}, errors.New("cursor is invalid")
	}
	if payload.Version != cursorVersion || len(payload.QueryDigest) != sha256.Size*2 || !validPhysicalIndex(payload.Generation) || payload.Score < 0 || payload.Score != payload.Score || payload.CreatedAt == "" || payload.PostID == 0 {
		return Cursor{}, errors.New("cursor is invalid")
	}
	if _, err := hex.DecodeString(payload.QueryDigest); err != nil {
		return Cursor{}, errors.New("cursor is invalid")
	}
	return Cursor{QueryDigest: payload.QueryDigest, Generation: payload.Generation, After: Hit{Score: payload.Score, CreatedAt: payload.CreatedAt, PostID: payload.PostID}}, nil
}

func digestQuery(query string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(query)))
	return hex.EncodeToString(digest[:])
}

func validationError(message string) error {
	return apperror.New(apperror.CodeValidationFailed, message)
}

func unavailableError() error {
	return apperror.New(apperror.CodeSearchUnavailable, "search is temporarily unavailable")
}
