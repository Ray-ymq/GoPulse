package search

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

const (
	DefaultLimit              = 20
	MaximumLimit              = 50
	cursorVersion             = 2
	maximumCursorBytes        = 8192
	pointInTimeKeepAliveValue = "2m"
)

const pointInTimeKeepAlive = 2 * time.Minute

type Options struct {
	Query  string
	Limit  int
	Cursor *Cursor
}

type Cursor struct {
	QueryDigest string
	Generation  string
	PointInTime string
	ExpiresAt   int64
	After       Hit
	signature   []byte
}

type cursorPayload struct {
	Version     int     `json:"v"`
	QueryDigest string  `json:"q"`
	Generation  string  `json:"generation"`
	PointInTime string  `json:"pit"`
	ExpiresAt   int64   `json:"expires_at"`
	Score       float64 `json:"score"`
	CreatedAt   string  `json:"created_at"`
	PostID      uint64  `json:"post_id"`
	ShardDoc    int64   `json:"shard_doc"`
}

type Searcher interface {
	ResolveGeneration(context.Context) (string, error)
	OpenPointInTime(context.Context, string) (string, error)
	Search(context.Context, string, string, string, int, *Hit) (SearchResult, error)
	ClosePointInTime(context.Context, string) error
}

type Hydrator interface {
	FindMany(context.Context, uint64, []uint64) ([]post.Post, error)
}

type Page struct {
	Posts      []post.Post
	NextCursor *string
}

type Service struct {
	searcher  Searcher
	hydrator  Hydrator
	cursorKey []byte
	now       func() time.Time
}

func NewService(searcher Searcher, hydrator Hydrator, cursorSecret string) *Service {
	return &Service{
		searcher:  searcher,
		hydrator:  hydrator,
		cursorKey: deriveCursorKey(cursorSecret),
		now:       time.Now,
	}
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
	if service == nil || service.searcher == nil || service.hydrator == nil || viewerID == 0 || len(service.cursorKey) == 0 || service.now == nil {
		return Page{}, apperror.WrapInternal(errors.New("search service is unavailable"))
	}

	queryDigest := digestQuery(options.Query)
	if options.Cursor != nil {
		if options.Cursor.QueryDigest != queryDigest || !verifyCursor(*options.Cursor, service.cursorKey) || service.now().Unix() >= options.Cursor.ExpiresAt {
			return Page{}, validationError("cursor is invalid")
		}
	}

	generation, err := service.searcher.ResolveGeneration(ctx)
	if err != nil {
		return Page{}, unavailableError()
	}

	pointInTime := ""
	var after *Hit
	if options.Cursor != nil {
		if options.Cursor.Generation != generation {
			_ = service.searcher.ClosePointInTime(ctx, options.Cursor.PointInTime)
			return Page{}, validationError("cursor is invalid")
		}
		pointInTime = options.Cursor.PointInTime
		after = &options.Cursor.After
	} else {
		pointInTime, err = service.searcher.OpenPointInTime(ctx, generation)
		if err != nil {
			return Page{}, unavailableError()
		}
	}

	result, err := service.searcher.Search(ctx, generation, pointInTime, options.Query, options.Limit, after)
	if err != nil {
		_ = service.searcher.ClosePointInTime(ctx, pointInTime)
		if errors.Is(err, ErrPointInTimeExpired) {
			return Page{}, validationError("cursor is invalid")
		}
		return Page{}, unavailableError()
	}
	if result.Generation != generation || result.PointInTime == "" {
		_ = service.searcher.ClosePointInTime(ctx, pointInTime)
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
			_ = service.searcher.ClosePointInTime(ctx, result.PointInTime)
			return Page{}, apperror.WrapInternal(errors.New("hydrate search results"))
		}
	}

	page := Page{Posts: records}
	if hasMore {
		last := visibleHits[len(visibleHits)-1]
		token, err := encodeCursor(Cursor{
			QueryDigest: queryDigest,
			Generation:  generation,
			PointInTime: result.PointInTime,
			ExpiresAt:   service.now().Add(pointInTimeKeepAlive).Unix(),
			After:       last,
		}, service.cursorKey)
		if err != nil {
			_ = service.searcher.ClosePointInTime(ctx, result.PointInTime)
			return Page{}, apperror.WrapInternal(err)
		}
		page.NextCursor = &token
	} else {
		_ = service.searcher.ClosePointInTime(ctx, result.PointInTime)
	}
	return page, nil
}

func EncodeCursor(cursor Cursor, secret string) (string, error) {
	return encodeCursor(cursor, deriveCursorKey(secret))
}

func encodeCursor(cursor Cursor, key []byte) (string, error) {
	payload, err := marshalCursor(cursor)
	if err != nil || len(key) == 0 {
		return "", errors.New("search cursor fields are invalid")
	}
	signature := signCursor(payload, key)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func DecodeCursor(token string) (Cursor, error) {
	if token == "" || len(token) > maximumCursorBytes {
		return Cursor{}, errors.New("cursor is invalid")
	}
	payloadToken, signatureToken, ok := strings.Cut(token, ".")
	if !ok || strings.Contains(signatureToken, ".") || payloadToken == "" || signatureToken == "" {
		return Cursor{}, errors.New("cursor is invalid")
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(payloadToken)
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != payloadToken {
		return Cursor{}, errors.New("cursor is invalid")
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(signatureToken)
	if err != nil || len(signature) != sha256.Size || base64.RawURLEncoding.EncodeToString(signature) != signatureToken {
		return Cursor{}, errors.New("cursor is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var raw cursorPayload
	if err := decoder.Decode(&raw); err != nil {
		return Cursor{}, errors.New("cursor is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Cursor{}, errors.New("cursor is invalid")
	}
	cursor := Cursor{
		QueryDigest: raw.QueryDigest,
		Generation:  raw.Generation,
		PointInTime: raw.PointInTime,
		ExpiresAt:   raw.ExpiresAt,
		After: Hit{
			Score: raw.Score, CreatedAt: raw.CreatedAt, PostID: raw.PostID, ShardDoc: raw.ShardDoc,
		},
		signature: signature,
	}
	canonical, err := marshalCursor(cursor)
	if err != nil || !bytes.Equal(canonical, payload) {
		return Cursor{}, errors.New("cursor is invalid")
	}
	return cursor, nil
}

func marshalCursor(cursor Cursor) ([]byte, error) {
	if cursor.QueryDigest == "" || !validPhysicalIndex(cursor.Generation) || cursor.PointInTime == "" || len(cursor.PointInTime) > 4096 || cursor.ExpiresAt <= 0 || cursor.After.PostID == 0 || cursor.After.CreatedAt == "" || cursor.After.Score < 0 || cursor.After.Score != cursor.After.Score || cursor.After.ShardDoc < 0 {
		return nil, errors.New("search cursor fields are invalid")
	}
	if len(cursor.QueryDigest) != sha256.Size*2 {
		return nil, errors.New("search cursor fields are invalid")
	}
	if _, err := hex.DecodeString(cursor.QueryDigest); err != nil {
		return nil, errors.New("search cursor fields are invalid")
	}
	return json.Marshal(cursorPayload{
		Version: cursorVersion, QueryDigest: cursor.QueryDigest, Generation: cursor.Generation,
		PointInTime: cursor.PointInTime, ExpiresAt: cursor.ExpiresAt,
		Score: cursor.After.Score, CreatedAt: cursor.After.CreatedAt, PostID: cursor.After.PostID, ShardDoc: cursor.After.ShardDoc,
	})
}

func verifyCursor(cursor Cursor, key []byte) bool {
	payload, err := marshalCursor(cursor)
	return err == nil && len(cursor.signature) == sha256.Size && hmac.Equal(cursor.signature, signCursor(payload, key))
}

func signCursor(payload, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func deriveCursorKey(secret string) []byte {
	if secret == "" {
		return nil
	}
	digest := sha256.Sum256([]byte("gopulse/search-cursor/v2\x00" + secret))
	return digest[:]
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
