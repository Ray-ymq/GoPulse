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

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/shared/apperror"
)

const (
	DefaultLimit              = 20
	MaximumLimit              = 50
	cursorVersion             = 2
	offsetCursorVersion       = 3
	cachedSearchPages         = 10
	maximumScanRounds         = 5
	maximumSearchOffset       = cachedSearchPages * maximumScanRounds * (MaximumLimit + 1)
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
	DataEpoch   string
	Generation  string
	PointInTime string
	ExpiresAt   int64
	After       Hit
	Page        int
	Offset      int
	Limit       int
	signature   []byte
}

type cursorPayload struct {
	Version     int     `json:"v"`
	QueryDigest string  `json:"q"`
	DataEpoch   string  `json:"epoch,omitempty"`
	Generation  string  `json:"generation"`
	PointInTime string  `json:"pit"`
	ExpiresAt   int64   `json:"expires_at"`
	Score       float64 `json:"score"`
	CreatedAt   string  `json:"created_at"`
	PostID      uint64  `json:"post_id"`
	ShardDoc    int64   `json:"shard_doc"`
}

type offsetCursorPayload struct {
	Version     int    `json:"v"`
	QueryDigest string `json:"q"`
	DataEpoch   string `json:"epoch,omitempty"`
	Generation  string `json:"generation"`
	ExpiresAt   int64  `json:"expires_at"`
	Page        int    `json:"page"`
	Offset      int    `json:"offset"`
	Limit       int    `json:"limit"`
}

type Searcher interface {
	ResolveGeneration(context.Context) (string, error)
	OpenPointInTime(context.Context, string) (string, error)
	Search(context.Context, string, string, string, int, *Hit) (SearchResult, error)
	SearchAtOffset(context.Context, string, string, string, int, int) (SearchResult, error)
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
	searcher      Searcher
	hydrator      Hydrator
	cursorKey     []byte
	resultCache   *resultCache
	now           func() time.Time
	epochProvider interface {
		CurrentEpoch(context.Context) (string, error)
	}
}

func NewService(searcher Searcher, hydrator Hydrator, cursorSecret string) *Service {
	return &Service{
		searcher:  searcher,
		hydrator:  hydrator,
		cursorKey: deriveCursorKey(cursorSecret),
		now:       time.Now,
	}
}

func (service *Service) WithEpochProvider(provider interface {
	CurrentEpoch(context.Context) (string, error)
}) *Service {
	if service != nil {
		service.epochProvider = provider
	}
	return service
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

	if options.Limit < 1 || options.Limit > MaximumLimit {
		return Page{}, validationError("limit must be an integer between 1 and 50")
	}
	query := strings.TrimSpace(options.Query)
	queryDigest := digestQuery(query)
	cursor := options.Cursor
	if cursor != nil && (cursor.QueryDigest != queryDigest || !verifyCursor(*cursor, service.cursorKey) ||
		service.now().Unix() >= cursor.ExpiresAt || (cursor.Page != 0 && cursor.Limit != options.Limit)) {
		return Page{}, validationError("cursor is invalid")
	}

	dataEpoch := ""
	var err error
	if service.epochProvider != nil {
		dataEpoch, err = service.epochProvider.CurrentEpoch(ctx)
		if err != nil || dataEpoch == "" {
			return Page{}, unavailableError()
		}
	}
	generation, err := service.resolveGeneration(ctx, dataEpoch)
	if err != nil {
		return Page{}, unavailableError()
	}

	pageNumber, offset := 1, 0
	pointInTime := ""
	var after *Hit
	if cursor != nil {
		if cursor.Generation != generation || ((cursor.Page != 0 || dataEpoch != "") && cursor.DataEpoch != dataEpoch) {
			if cursor.PointInTime != "" {
				_ = service.searcher.ClosePointInTime(ctx, cursor.PointInTime)
			}
			return Page{}, validationError("cursor is invalid")
		}
		if cursor.Page == 0 {
			pointInTime, after = cursor.PointInTime, &cursor.After
		} else {
			pageNumber, offset = cursor.Page, cursor.Offset
		}
	}
	if pageNumber > cachedSearchPages {
		pointInTime, err = service.searcher.OpenPointInTime(ctx, generation)
		if err != nil || pointInTime == "" {
			return Page{}, unavailableError()
		}
	}
	keepPointInTime := false
	defer func() {
		if pointInTime != "" && !keepPointInTime {
			_ = service.searcher.ClosePointInTime(ctx, pointInTime)
		}
	}()

	cacheKey := ""
	if service.resultCache != nil && pointInTime == "" && validEpoch(dataEpoch) {
		cacheKey = resultCacheKey(dataEpoch, generation, queryDigest, pageNumber, offset, options.Limit)
	}
	page := Page{Posts: make([]post.Post, 0, options.Limit)}
	hasMore := false
	var last Hit
	for round := 0; round < maximumScanRounds; round++ {
		var ids []uint64
		var result SearchResult
		if round == 0 && cacheKey != "" {
			ids = service.resultCache.get(ctx, cacheKey, options.Limit)
		}
		cacheHit := len(ids) > 0
		if !cacheHit {
			if after != nil {
				result, err = service.searcher.Search(ctx, generation, pointInTime, query, options.Limit, after)
			} else {
				result, err = service.searcher.SearchAtOffset(ctx, generation, pointInTime, query, options.Limit, offset)
			}
			if errors.Is(err, ErrPointInTimeExpired) {
				return Page{}, validationError("cursor is invalid")
			}
			if err != nil || result.Generation != generation || (pointInTime == "") != (result.PointInTime == "") {
				return Page{}, unavailableError()
			}
			pointInTime = result.PointInTime
			ids = make([]uint64, len(result.Hits))
			for i, hit := range result.Hits {
				ids[i] = hit.PostID
			}
			if round == 0 && cacheKey != "" && len(ids) > 0 {
				service.resultCache.set(ctx, cacheKey, ids)
			}
		}

		byID, err := service.hydrate(ctx, viewerID, ids)
		if err != nil {
			return Page{}, err
		}
		consumed := 0
		for _, id := range ids {
			if record, exists := byID[id]; exists {
				if len(page.Posts) == options.Limit {
					break
				}
				page.Posts = append(page.Posts, record)
			}
			consumed++
		}
		offset += consumed
		if pointInTime != "" && consumed > 0 {
			last = result.Hits[consumed-1]
			after = &last
		}
		hasMore = consumed < len(ids) || len(ids) > options.Limit
		refill := cacheHit && len(byID) < len(ids) && len(page.Posts) < options.Limit
		if consumed < len(ids) || (!hasMore && !refill) {
			break
		}
	}
	if hasMore {
		next := Cursor{
			QueryDigest: queryDigest,
			DataEpoch:   dataEpoch,
			Generation:  generation,
			PointInTime: pointInTime,
			ExpiresAt:   service.now().Add(pointInTimeKeepAlive).Unix(),
			After:       last,
		}
		if pointInTime == "" {
			next.Page, next.Offset, next.Limit = pageNumber+1, offset, options.Limit
		}
		token, err := encodeCursor(next, service.cursorKey)
		if err != nil {
			return Page{}, apperror.WrapInternal(err)
		}
		page.NextCursor = &token
		keepPointInTime = true
	}
	return page, nil
}

func (service *Service) hydrate(ctx context.Context, viewerID uint64, ids []uint64) (map[uint64]post.Post, error) {
	byID := make(map[uint64]post.Post, len(ids))
	for start := 0; start < len(ids); start += post.MaximumLimit {
		end := min(start+post.MaximumLimit, len(ids))
		records, err := service.hydrator.FindMany(ctx, viewerID, ids[start:end])
		if err != nil {
			return nil, apperror.WrapInternal(errors.New("hydrate search results"))
		}
		for _, record := range records {
			byID[record.ID] = record
		}
	}
	return byID, nil
}

func (service *Service) resolveGeneration(ctx context.Context, epoch string) (string, error) {
	if epoch != "" {
		if resolver, ok := service.searcher.(interface {
			ResolveGenerationForEpoch(context.Context, string) (string, error)
		}); ok {
			return resolver.ResolveGenerationForEpoch(ctx, epoch)
		}
	}
	return service.searcher.ResolveGeneration(ctx)
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
	var raw struct {
		cursorPayload
		Page   int `json:"page"`
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if err := decoder.Decode(&raw); err != nil {
		return Cursor{}, errors.New("cursor is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Cursor{}, errors.New("cursor is invalid")
	}
	cursor := Cursor{
		QueryDigest: raw.QueryDigest,
		DataEpoch:   raw.DataEpoch,
		Generation:  raw.Generation,
		PointInTime: raw.PointInTime,
		ExpiresAt:   raw.ExpiresAt,
		After: Hit{
			Score: raw.Score, CreatedAt: raw.CreatedAt, PostID: raw.PostID, ShardDoc: raw.ShardDoc,
		},
		Page: raw.Page, Offset: raw.Offset, Limit: raw.Limit,
		signature: signature,
	}
	canonical, err := marshalCursor(cursor)
	if err != nil || !bytes.Equal(canonical, payload) {
		return Cursor{}, errors.New("cursor is invalid")
	}
	return cursor, nil
}

func marshalCursor(cursor Cursor) ([]byte, error) {
	if len(cursor.QueryDigest) != sha256.Size*2 || !validPhysicalIndex(cursor.Generation) || cursor.ExpiresAt <= 0 {
		return nil, errors.New("search cursor fields are invalid")
	}
	if _, err := hex.DecodeString(cursor.QueryDigest); err != nil {
		return nil, errors.New("search cursor fields are invalid")
	}
	if cursor.DataEpoch != "" && !validEpoch(cursor.DataEpoch) {
		return nil, errors.New("search cursor fields are invalid")
	}
	if cursor.Page != 0 {
		if cursor.Page < 2 || cursor.Page > cachedSearchPages+1 || cursor.Limit < 1 || cursor.Limit > MaximumLimit ||
			cursor.Offset < cursor.Page-1 || cursor.Offset > (cursor.Page-1)*maximumScanRounds*(cursor.Limit+1) ||
			cursor.PointInTime != "" || cursor.After != (Hit{}) {
			return nil, errors.New("search cursor fields are invalid")
		}
		return json.Marshal(offsetCursorPayload{
			Version: offsetCursorVersion, QueryDigest: cursor.QueryDigest, DataEpoch: cursor.DataEpoch, Generation: cursor.Generation,
			ExpiresAt: cursor.ExpiresAt, Page: cursor.Page, Offset: cursor.Offset, Limit: cursor.Limit,
		})
	}
	if cursor.Offset != 0 || cursor.Limit != 0 || cursor.PointInTime == "" || len(cursor.PointInTime) > 4096 ||
		cursor.After.PostID == 0 || cursor.After.CreatedAt == "" || cursor.After.Score < 0 || cursor.After.Score != cursor.After.Score || cursor.After.ShardDoc < 0 {
		return nil, errors.New("search cursor fields are invalid")
	}
	return json.Marshal(cursorPayload{
		Version: cursorVersion, QueryDigest: cursor.QueryDigest, DataEpoch: cursor.DataEpoch, Generation: cursor.Generation,
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
