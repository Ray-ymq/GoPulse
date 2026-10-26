package logquery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/Ray-ymq/GoPulse/runtimekit"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

const (
	ReadAlias          = "gopulse-logs-v1-read"
	RetentionDays      = 7
	RetentionWindow    = RetentionDays * 24 * time.Hour
	DefaultLimit       = 50
	MaximumLimit       = 100
	pitKeepAlive       = "2m"
	maximumCursorBytes = 8192
)

var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var eventIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var dataEpochPattern = eventIDPattern
var traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var spanIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
var attemptIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var boundedTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,255}$`)

var ErrUnavailable = errors.New("logs unavailable")
var ErrAliasMissing = errors.New("log alias missing")
var ErrPITExpired = errors.New("log PIT expired")

type Filters struct {
	From                    string `json:"from"`
	To                      string `json:"to"`
	Service                 string `json:"service,omitempty"`
	Module                  string `json:"module,omitempty"`
	Level                   string `json:"level,omitempty"`
	Message                 string `json:"message,omitempty"`
	RequestID               string `json:"request_id,omitempty"`
	TraceID                 string `json:"trace_id,omitempty"`
	SpanID                  string `json:"span_id,omitempty"`
	EventID                 string `json:"event_id,omitempty"`
	CommandID               string `json:"command_id,omitempty"`
	DataEpoch               string `json:"data_epoch,omitempty"`
	PostID                  string `json:"post_id,omitempty"`
	ProjectionGeneration    string `json:"projection_generation,omitempty"`
	RoutingContractRevision string `json:"routing_contract_revision,omitempty"`
	Consumer                string `json:"consumer,omitempty"`
	ErrorCode               string `json:"error_code,omitempty"`
}
type Options struct {
	Filters Filters
	Limit   int
	Cursor  string
}
type Sort struct {
	Timestamp string `json:"timestamp"`
	ShardDoc  int64  `json:"shard_doc"`
}
type cursorPayload struct {
	Version   int     `json:"v"`
	Filters   Filters `json:"filters"`
	Limit     int     `json:"limit"`
	PIT       string  `json:"pit"`
	ExpiresAt int64   `json:"expires_at"`
	After     Sort    `json:"after"`
}

type Entry struct {
	Timestamp               string  `json:"timestamp"`
	Level                   string  `json:"level"`
	Service                 string  `json:"service"`
	InstanceID              string  `json:"instance_id,omitempty"`
	Module                  string  `json:"module"`
	Message                 string  `json:"message"`
	RequestID               string  `json:"request_id,omitempty"`
	TraceID                 string  `json:"trace_id,omitempty"`
	SpanID                  string  `json:"span_id,omitempty"`
	AttemptID               string  `json:"attempt_id,omitempty"`
	EventID                 string  `json:"event_id,omitempty"`
	EventType               string  `json:"event_type,omitempty"`
	CommandID               string  `json:"command_id,omitempty"`
	DataEpoch               string  `json:"data_epoch,omitempty"`
	ProjectionGeneration    string  `json:"projection_generation,omitempty"`
	RoutingContractRevision string  `json:"routing_contract_revision,omitempty"`
	Consumer                string  `json:"consumer,omitempty"`
	UserID                  *uint64 `json:"user_id,omitempty"`
	PostID                  *uint64 `json:"post_id,omitempty"`
	ContentRevision         *uint64 `json:"content_revision,omitempty"`
	CommentID               *uint64 `json:"comment_id,omitempty"`
	NotificationID          *uint64 `json:"notification_id,omitempty"`
	OutboxID                *uint64 `json:"outbox_id,omitempty"`
	Method                  string  `json:"method,omitempty"`
	Route                   string  `json:"route,omitempty"`
	Status                  *int64  `json:"status,omitempty"`
	DurationMS              *int64  `json:"duration_ms,omitempty"`
	ResponseBytes           *int64  `json:"response_bytes,omitempty"`
	ErrorCode               string  `json:"error_code,omitempty"`
	Reason                  string  `json:"reason,omitempty"`
	Operation               string  `json:"operation,omitempty"`
	Resource                string  `json:"resource,omitempty"`
	Stage                   string  `json:"stage,omitempty"`
	Result                  string  `json:"result,omitempty"`
	Attempt                 *int64  `json:"attempt,omitempty"`
	BatchSize               *int64  `json:"batch_size,omitempty"`
	DocumentCount           *int64  `json:"document_count,omitempty"`
	PanicRecovered          *bool   `json:"panic_recovered,omitempty"`
	ResponseCommitted       *bool   `json:"response_committed,omitempty"`
}
type Hit struct {
	Entry Entry
	Sort  Sort
}
type SearchResult struct {
	PIT  string
	Hits []Hit
}
type Page struct {
	Entries    []Entry
	NextCursor *string
}

// CorrelationQuery is the internal, typed log association contract used by
// business-stage reads. It cannot carry a caller supplied DSL or free-form
// field list and is deliberately capped at one bounded backend request.
type CorrelationQuery struct {
	DataEpoch string
	CommandID string
	EventID   string
	PostID    uint64
	From      time.Time
	To        time.Time
	Limit     int
}

type CorrelationResult struct {
	Entries   []Entry
	Truncated bool
	Quality   string
}

type CorrelationRepository interface {
	SearchCorrelation(context.Context, CorrelationQuery) (CorrelationResult, error)
}

type Repository interface {
	OpenPointInTime(context.Context) (string, error)
	Search(context.Context, string, Filters, int, *Sort) (SearchResult, error)
	ClosePointInTime(context.Context, string) error
}

type Service struct {
	repository Repository
	key        []byte
	now        func() time.Time
}

func NewService(repository Repository, secret string) *Service {
	return &Service{repository: repository, key: deriveKey(secret), now: time.Now}
}

func ParseOptions(values url.Values, now time.Time) (Options, error) {
	allowed := map[string]bool{"from": true, "to": true, "service": true, "module": true, "level": true, "message": true, "request_id": true, "trace_id": true, "span_id": true, "event_id": true, "command_id": true, "data_epoch": true, "post_id": true, "projection_generation": true, "routing_contract_revision": true, "consumer": true, "error_code": true, "limit": true, "cursor": true}
	for key, list := range values {
		if !allowed[key] || len(list) != 1 || list[0] == "" || !utf8.ValidString(list[0]) {
			return Options{}, validation()
		}
	}
	if cursor, ok := values["cursor"]; ok {
		if len(values) != 1 {
			return Options{}, validation()
		}
		if len(cursor[0]) > maximumCursorBytes {
			return Options{}, validation()
		}
		return Options{Cursor: cursor[0]}, nil
	}
	to := now.UTC()
	from := to.Add(-15 * time.Minute)
	var err error
	if raw, ok := values["to"]; ok {
		to, err = time.Parse(time.RFC3339Nano, raw[0])
		if err != nil || to.Location() != time.UTC {
			return Options{}, validation()
		}
	}
	if raw, ok := values["from"]; ok {
		from, err = time.Parse(time.RFC3339Nano, raw[0])
		if err != nil || from.Location() != time.UTC {
			return Options{}, validation()
		}
	}
	// Keep the existing one-day page bound. A caller may still ask for a
	// one-day slice outside the seven-day storage window; the fixed read alias
	// then returns an empty page after lifecycle cleanup.
	if !from.Before(to) || to.Sub(from) > 24*time.Hour || to.After(now.UTC().Add(5*time.Minute)) {
		return Options{}, validation()
	}
	options := Options{Limit: DefaultLimit, Filters: Filters{From: from.Format(time.RFC3339Nano), To: to.Format(time.RFC3339Nano)}}
	if raw, ok := values["limit"]; ok {
		options.Limit, err = strconv.Atoi(raw[0])
		if err != nil || options.Limit < 1 || options.Limit > MaximumLimit {
			return Options{}, validation()
		}
	}
	options.Filters.Service = single(values, "service")
	options.Filters.Module = single(values, "module")
	options.Filters.Level = single(values, "level")
	options.Filters.Message = single(values, "message")
	options.Filters.RequestID = single(values, "request_id")
	options.Filters.TraceID = single(values, "trace_id")
	options.Filters.SpanID = single(values, "span_id")
	options.Filters.EventID = single(values, "event_id")
	options.Filters.CommandID = single(values, "command_id")
	options.Filters.DataEpoch = single(values, "data_epoch")
	options.Filters.PostID = single(values, "post_id")
	options.Filters.ProjectionGeneration = single(values, "projection_generation")
	options.Filters.RoutingContractRevision = single(values, "routing_contract_revision")
	options.Filters.Consumer = single(values, "consumer")
	options.Filters.ErrorCode = single(values, "error_code")
	if !validLogVocabulary(options.Filters.Service, options.Filters.Module, options.Filters.Message) {
		return Options{}, validation()
	}
	if !validLevel(options.Filters.Level) {
		return Options{}, validation()
	}
	if options.Filters.ErrorCode != "" && !validErrorCode(options.Filters.ErrorCode) {
		return Options{}, validation()
	}
	if options.Filters.RequestID != "" && !requestIDPattern.MatchString(options.Filters.RequestID) {
		return Options{}, validation()
	}
	if options.Filters.EventID != "" && !eventIDPattern.MatchString(options.Filters.EventID) {
		return Options{}, validation()
	}
	if options.Filters.CommandID != "" && !eventIDPattern.MatchString(options.Filters.CommandID) {
		return Options{}, validation()
	}
	if options.Filters.DataEpoch != "" && !dataEpochPattern.MatchString(options.Filters.DataEpoch) {
		return Options{}, validation()
	}
	if options.Filters.PostID != "" {
		postID, parseErr := strconv.ParseUint(options.Filters.PostID, 10, 64)
		if parseErr != nil || postID == 0 {
			return Options{}, validation()
		}
	}
	for _, value := range []string{options.Filters.ProjectionGeneration, options.Filters.RoutingContractRevision, options.Filters.Consumer} {
		if value != "" && !boundedTokenPattern.MatchString(value) {
			return Options{}, validation()
		}
	}
	if options.Filters.TraceID != "" && (!traceIDPattern.MatchString(options.Filters.TraceID) || allZero(options.Filters.TraceID)) {
		return Options{}, validation()
	}
	if options.Filters.SpanID != "" && (!spanIDPattern.MatchString(options.Filters.SpanID) || allZero(options.Filters.SpanID)) {
		return Options{}, validation()
	}
	return options, nil
}
func single(values url.Values, key string) string {
	if list, ok := values[key]; ok {
		return list[0]
	}
	return ""
}

func (s *Service) Query(ctx context.Context, options Options) (Page, error) {
	var payload cursorPayload
	if options.Cursor != "" {
		var err error
		payload, err = s.decode(options.Cursor)
		if err != nil {
			return Page{}, validation()
		}
	} else {
		pit, err := s.repository.OpenPointInTime(ctx)
		if errors.Is(err, ErrAliasMissing) {
			return Page{Entries: []Entry{}}, nil
		}
		if err != nil {
			return Page{}, unavailable()
		}
		payload = cursorPayload{Version: 1, Filters: options.Filters, Limit: options.Limit, PIT: pit, ExpiresAt: s.now().Add(2 * time.Minute).Unix()}
	}
	var after *Sort
	if payload.After.Timestamp != "" {
		after = &payload.After
	}
	result, err := s.repository.Search(ctx, payload.PIT, payload.Filters, payload.Limit, after)
	if errors.Is(err, ErrPITExpired) {
		return Page{}, validation()
	}
	if err != nil {
		return Page{}, unavailable()
	}
	entries := make([]Entry, 0, min(len(result.Hits), payload.Limit))
	for i, hit := range result.Hits {
		if i == payload.Limit {
			break
		}
		entries = append(entries, hit.Entry)
	}
	if len(result.Hits) <= payload.Limit {
		_ = s.repository.ClosePointInTime(ctx, result.PIT)
		return Page{Entries: entries}, nil
	}
	last := result.Hits[payload.Limit-1].Sort
	payload.PIT = result.PIT
	payload.After = last
	payload.ExpiresAt = s.now().Add(2 * time.Minute).Unix()
	cursor, err := s.encode(payload)
	if err != nil {
		return Page{}, unavailable()
	}
	return Page{Entries: entries, NextCursor: &cursor}, nil
}

func (s *Service) QueryCorrelation(ctx context.Context, query CorrelationQuery) (CorrelationResult, error) {
	if s == nil || s.repository == nil || ctx == nil || !dataEpochPattern.MatchString(query.DataEpoch) || query.Limit < 1 || query.Limit > MaximumLimit || query.From.IsZero() || query.To.IsZero() || query.From.Location() != time.UTC || query.To.Location() != time.UTC || !query.From.Before(query.To) || query.To.Sub(query.From) > 24*time.Hour {
		return CorrelationResult{}, validation()
	}
	selectors := 0
	if query.CommandID != "" {
		selectors++
		if !eventIDPattern.MatchString(query.CommandID) {
			return CorrelationResult{}, validation()
		}
	}
	if query.EventID != "" {
		selectors++
		if !eventIDPattern.MatchString(query.EventID) {
			return CorrelationResult{}, validation()
		}
	}
	if query.PostID != 0 {
		selectors++
	}
	if selectors != 1 {
		return CorrelationResult{}, validation()
	}
	searcher, ok := s.repository.(CorrelationRepository)
	if !ok {
		return CorrelationResult{}, unavailable()
	}
	result, err := searcher.SearchCorrelation(ctx, query)
	if errors.Is(err, ErrAliasMissing) {
		return CorrelationResult{Entries: []Entry{}, Quality: "expired"}, nil
	}
	if err != nil {
		return CorrelationResult{}, unavailable()
	}
	if result.Quality == "" {
		result.Quality = "no_evidence"
		if len(result.Entries) > 0 {
			result.Quality = "known"
		}
	}
	return result, nil
}

func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte("gopulse/backend/log-query-cursor/v1\x00" + secret))
	return sum[:]
}
func (s *Service) encode(payload cursorPayload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write(body)
	signed := append(body, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(signed), nil
}
func (s *Service) decode(value string) (cursorPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) <= sha256.Size || len(raw) > maximumCursorBytes {
		return cursorPayload{}, errors.New("invalid cursor")
	}
	body, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, s.key)
	mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return cursorPayload{}, errors.New("invalid cursor")
	}
	var payload cursorPayload
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || payload.Version != 1 || payload.PIT == "" || payload.Limit < 1 || payload.Limit > MaximumLimit || payload.ExpiresAt < s.now().Unix() {
		return cursorPayload{}, errors.New("invalid cursor")
	}
	return payload, nil
}
func validation() error {
	return apperror.New(apperror.CodeValidationFailed, "log query parameters are invalid")
}
func unavailable() error {
	return apperror.New(apperror.CodeLogsUnavailable, "logs are temporarily unavailable")
}

// ElasticsearchRepository uses only the fixed log read alias and a server-owned query body.
type Performer interface {
	Perform(context.Context, *http.Request) (*http.Response, error)
}
type ElasticsearchRepository struct{ client Performer }

func NewElasticsearchRepository(client Performer) *ElasticsearchRepository {
	return &ElasticsearchRepository{client: client}
}
func (r *ElasticsearchRepository) OpenPointInTime(ctx context.Context) (string, error) {
	response, err := r.do(ctx, http.MethodPost, "/"+ReadAlias+"/_pit?keep_alive="+url.QueryEscape(pitKeepAlive)+"&allow_partial_search_results=false", nil)
	if err != nil {
		return "", ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return "", ErrAliasMissing
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", ErrUnavailable
	}
	var payload struct {
		ID string `json:"id"`
	}
	if decodeLimited(response.Body, &payload) != nil || payload.ID == "" || len(payload.ID) > 4096 {
		return "", ErrUnavailable
	}
	return payload.ID, nil
}
func (r *ElasticsearchRepository) Search(ctx context.Context, pit string, filters Filters, limit int, after *Sort) (SearchResult, error) {
	if pit == "" || len(pit) > 4096 || limit < 1 || limit > MaximumLimit {
		return SearchResult{}, ErrUnavailable
	}
	must := []any{map[string]any{"range": map[string]any{"@timestamp": map[string]string{"gte": filters.From, "lt": filters.To}}}}
	for field, value := range map[string]string{"service": filters.Service, "module": filters.Module, "level": filters.Level, "message": filters.Message, "request_id": filters.RequestID, "trace_id": filters.TraceID, "span_id": filters.SpanID, "event_id": filters.EventID, "command_id": filters.CommandID, "data_epoch": filters.DataEpoch, "post_id": filters.PostID, "projection_generation": filters.ProjectionGeneration, "routing_contract_revision": filters.RoutingContractRevision, "consumer": filters.Consumer, "error_code": filters.ErrorCode} {
		if value != "" {
			must = append(must, map[string]any{"term": map[string]string{field: value}})
		}
	}
	body := map[string]any{"size": limit + 1, "pit": map[string]string{"id": pit, "keep_alive": pitKeepAlive}, "query": map[string]any{"bool": map[string]any{"filter": must}}, "sort": []any{map[string]any{"@timestamp": map[string]string{"order": "desc", "format": "strict_date_optional_time_nanos"}}, map[string]any{"_shard_doc": map[string]string{"order": "desc"}}}}
	if after != nil {
		body["search_after"] = []any{after.Timestamp, after.ShardDoc}
	}
	encoded, _ := json.Marshal(body)
	response, err := r.do(ctx, http.MethodPost, "/_search", bytes.NewReader(encoded))
	if err != nil {
		return SearchResult{}, ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return SearchResult{}, ErrPITExpired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return SearchResult{}, ErrUnavailable
	}
	var payload struct {
		PIT  string `json:"pit_id"`
		Hits struct {
			Hits []struct {
				Source json.RawMessage   `json:"_source"`
				Sort   []json.RawMessage `json:"sort"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if decodeLimited(response.Body, &payload) != nil || payload.PIT == "" || len(payload.PIT) > 4096 {
		return SearchResult{}, ErrUnavailable
	}
	result := SearchResult{PIT: payload.PIT, Hits: make([]Hit, 0, len(payload.Hits.Hits))}
	for _, raw := range payload.Hits.Hits {
		if len(raw.Sort) != 2 {
			return SearchResult{}, ErrUnavailable
		}
		entry, err := decodeEntry(raw.Source)
		if err != nil {
			return SearchResult{}, ErrUnavailable
		}
		var sort Sort
		if json.Unmarshal(raw.Sort[0], &sort.Timestamp) != nil {
			return SearchResult{}, ErrUnavailable
		}
		var number json.Number
		if json.Unmarshal(raw.Sort[1], &number) != nil {
			return SearchResult{}, ErrUnavailable
		}
		sort.ShardDoc, err = strconv.ParseInt(number.String(), 10, 64)
		if err != nil {
			return SearchResult{}, ErrUnavailable
		}
		result.Hits = append(result.Hits, Hit{Entry: entry, Sort: sort})
	}
	return result, nil
}

func (r *ElasticsearchRepository) SearchCorrelation(ctx context.Context, query CorrelationQuery) (CorrelationResult, error) {
	if !dataEpochPattern.MatchString(query.DataEpoch) || query.Limit < 1 || query.Limit > MaximumLimit || query.From.IsZero() || query.To.IsZero() {
		return CorrelationResult{}, ErrUnavailable
	}
	filters := []any{map[string]any{"range": map[string]any{"@timestamp": map[string]string{"gte": query.From.Format(time.RFC3339Nano), "lt": query.To.Format(time.RFC3339Nano)}}}}
	// A v1 log may predate epoch propagation. It remains a historical clue
	// only when its exact durable identity matches; it is never treated as a
	// current-epoch execution proof.
	filters = append(filters, map[string]any{"bool": map[string]any{"should": []any{
		map[string]any{"term": map[string]string{"data_epoch": query.DataEpoch}},
		map[string]any{"bool": map[string]any{"must_not": []any{map[string]any{"exists": map[string]string{"field": "data_epoch"}}}}},
	}, "minimum_should_match": 1}})
	if query.CommandID != "" {
		filters = append(filters, map[string]any{"term": map[string]string{"command_id": query.CommandID}})
	}
	if query.EventID != "" {
		filters = append(filters, map[string]any{"term": map[string]string{"event_id": query.EventID}})
	}
	if query.PostID != 0 {
		filters = append(filters, map[string]any{"term": map[string]any{"post_id": query.PostID}})
	}
	body, err := json.Marshal(map[string]any{
		"size":  query.Limit + 1,
		"query": map[string]any{"bool": map[string]any{"filter": filters}},
		"sort":  []any{map[string]any{"@timestamp": map[string]string{"order": "desc", "format": "strict_date_optional_time_nanos"}}, map[string]any{"_shard_doc": map[string]string{"order": "desc"}}},
	})
	if err != nil {
		return CorrelationResult{}, ErrUnavailable
	}
	response, err := r.do(ctx, http.MethodPost, "/"+ReadAlias+"/_search", bytes.NewReader(body))
	if err != nil {
		return CorrelationResult{}, ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return CorrelationResult{}, ErrAliasMissing
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return CorrelationResult{}, ErrUnavailable
	}
	var payload struct {
		Hits struct {
			Hits []struct {
				Source json.RawMessage `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := decodeLimited(response.Body, &payload); err != nil {
		return CorrelationResult{}, ErrUnavailable
	}
	result := CorrelationResult{Entries: make([]Entry, 0, min(len(payload.Hits.Hits), query.Limit))}
	hasLegacy := false
	for index, hit := range payload.Hits.Hits {
		if index >= query.Limit {
			result.Truncated = true
			break
		}
		entry, err := decodeEntry(hit.Source)
		if err != nil {
			return CorrelationResult{}, ErrUnavailable
		}
		if entry.DataEpoch != "" && entry.DataEpoch != query.DataEpoch {
			return CorrelationResult{}, ErrUnavailable
		}
		if entry.DataEpoch == "" {
			hasLegacy = true
		}
		result.Entries = append(result.Entries, entry)
	}
	if len(result.Entries) == 0 {
		result.Quality = "no_evidence"
	} else if hasLegacy {
		result.Quality = "legacy_unknown"
	} else {
		result.Quality = "known"
	}
	return result, nil
}
func (r *ElasticsearchRepository) ClosePointInTime(ctx context.Context, pit string) error {
	encoded, _ := json.Marshal(map[string]string{"id": pit})
	response, err := r.do(ctx, http.MethodDelete, "/_pit", bytes.NewReader(encoded))
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrUnavailable
	}
	return nil
}
func (r *ElasticsearchRepository) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	request, err := runtimekit.NewRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return r.client.Perform(ctx, request)
}
func decodeLimited(body io.Reader, destination any) error {
	limited := io.LimitReader(body, 1<<20)
	return json.NewDecoder(limited).Decode(destination)
}
func decodeEntry(source []byte) (Entry, error) {
	var raw struct {
		Timestamp        string `json:"@timestamp"`
		LogSchemaVersion int    `json:"log_schema_version"`
		// Runtime metadata remains persisted; decode its supported fields explicitly
		// while exposing instance identity in the public page entry.
		Version                string  `json:"version"`
		Revision               string  `json:"revision"`
		Event                  string  `json:"event"`
		RuntimeContractVersion string  `json:"runtime_contract_version"`
		RuntimeMode            string  `json:"runtime_mode"`
		Listen                 string  `json:"listen"`
		InstanceID             *string `json:"instance_id"`
		Entry
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil || raw.Timestamp == "" || raw.LogSchemaVersion != 1 {
		return Entry{}, errors.New("invalid log document")
	}
	if raw.InstanceID != nil {
		if runtimekit.ValidateInstanceID(*raw.InstanceID) != nil {
			return Entry{}, errors.New("invalid log document")
		}
		raw.Entry.InstanceID = *raw.InstanceID
	}
	if raw.TraceID != "" && (!traceIDPattern.MatchString(raw.TraceID) || allZero(raw.TraceID)) {
		return Entry{}, errors.New("invalid log document")
	}
	if raw.SpanID != "" && (!spanIDPattern.MatchString(raw.SpanID) || allZero(raw.SpanID)) {
		return Entry{}, errors.New("invalid log document")
	}
	if raw.AttemptID != "" && (!attemptIDPattern.MatchString(raw.AttemptID) || allZero(raw.AttemptID)) {
		return Entry{}, errors.New("invalid log document")
	}
	if raw.DataEpoch != "" && !dataEpochPattern.MatchString(raw.DataEpoch) {
		return Entry{}, errors.New("invalid log document")
	}
	if raw.CommandID != "" && !eventIDPattern.MatchString(raw.CommandID) {
		return Entry{}, errors.New("invalid log document")
	}
	for _, value := range []string{raw.ProjectionGeneration, raw.RoutingContractRevision, raw.Consumer} {
		if value != "" && !boundedTokenPattern.MatchString(value) {
			return Entry{}, errors.New("invalid log document")
		}
	}
	if raw.ContentRevision != nil && *raw.ContentRevision == 0 {
		return Entry{}, errors.New("invalid log document")
	}
	raw.Entry.Timestamp = raw.Timestamp
	return raw.Entry, nil
}

func allZero(value string) bool {
	for _, character := range value {
		if character != '0' {
			return false
		}
	}
	return true
}
