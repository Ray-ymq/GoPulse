package logquery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	DefaultLimit       = 50
	MaximumLimit       = 100
	pitKeepAlive       = "2m"
	maximumCursorBytes = 8192
)

var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var eventIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

var ErrUnavailable = errors.New("logs unavailable")
var ErrAliasMissing = errors.New("log alias missing")
var ErrPITExpired = errors.New("log PIT expired")

// logVocabulary mirrors the Schema v1 source/module/message contract enforced
// by LogMonitor and Marshaller. Query filters may only select combinations that
// a valid application log can contain.
var workerMessages = map[string]struct{}{
	"event ignored": {}, "event processed": {}, "message acknowledgement failed": {},
	"retry publish failed": {}, "message requeue failed": {}, "event retry scheduled": {},
	"dead letter publish failed": {}, "event dead lettered": {}, "connection unavailable": {},
	"connection restored": {}, "session close failed": {}, "session interrupted": {},
	"delivery stop failed": {}, "shutdown timeout": {},
}

var logVocabulary = map[string]map[string]map[string]struct{}{
	"backend": {
		"http": {"request id generation failed": {}, "http request completed": {}, "http panic recovered": {}},
		"auth": {"user registered": {}, "user logged in": {}, "user logged out": {}},
		"post": {"post created": {}}, "comment": {"comment created": {}},
		"like": {"post liked": {}, "post unliked": {}}, "notification": {"notification marked read": {}},
		"cache":     {"post detail cache fill failed": {}, "post detail cache read failed": {}, "post detail cache invalidation failed": {}},
		"outbox":    {"outbox cleanup failed": {}, "outbox claim failed": {}, "outbox event invalid": {}, "outbox publish failed": {}, "outbox mark published failed": {}, "outbox event published": {}, "outbox release failed": {}},
		"lifecycle": {"backend listening": {}, "backend stopped": {}, "backend server failed": {}, "backend shutdown started": {}, "backend shutdown failed": {}, "resource close failed": {}},
	},
	"business-worker": {
		"lifecycle": {"business worker started": {}, "business worker stopped": {}, "business worker initialization failed": {}, "resource close failed": {}},
		"worker":    workerMessages, "notification": workerMessages,
	},
	"search-indexer": {
		"lifecycle": {"search indexer started": {}, "search indexer stopped": {}, "search indexer initialization failed": {}, "resource close failed": {}},
		"worker":    workerMessages, "search": workerMessages,
	},
	"search-reindex": {
		"search": {"search reindex arguments invalid": {}, "search reindex initialization failed": {}, "search reindex started": {}, "search reindex skipped": {}, "search reindex completed": {}, "search reindex failed": {}, "resource close failed": {}},
	},
}

type Filters struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Service   string `json:"service,omitempty"`
	Module    string `json:"module,omitempty"`
	Level     string `json:"level,omitempty"`
	Message   string `json:"message,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	EventID   string `json:"event_id,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
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
	Timestamp         string  `json:"timestamp"`
	Level             string  `json:"level"`
	Service           string  `json:"service"`
	Module            string  `json:"module"`
	Message           string  `json:"message"`
	RequestID         string  `json:"request_id,omitempty"`
	EventID           string  `json:"event_id,omitempty"`
	EventType         string  `json:"event_type,omitempty"`
	UserID            *uint64 `json:"user_id,omitempty"`
	PostID            *uint64 `json:"post_id,omitempty"`
	CommentID         *uint64 `json:"comment_id,omitempty"`
	NotificationID    *uint64 `json:"notification_id,omitempty"`
	OutboxID          *uint64 `json:"outbox_id,omitempty"`
	Method            string  `json:"method,omitempty"`
	Route             string  `json:"route,omitempty"`
	Status            *int64  `json:"status,omitempty"`
	DurationMS        *int64  `json:"duration_ms,omitempty"`
	ResponseBytes     *int64  `json:"response_bytes,omitempty"`
	ErrorCode         string  `json:"error_code,omitempty"`
	Reason            string  `json:"reason,omitempty"`
	Operation         string  `json:"operation,omitempty"`
	Resource          string  `json:"resource,omitempty"`
	Stage             string  `json:"stage,omitempty"`
	Result            string  `json:"result,omitempty"`
	Attempt           *int64  `json:"attempt,omitempty"`
	BatchSize         *int64  `json:"batch_size,omitempty"`
	DocumentCount     *int64  `json:"document_count,omitempty"`
	PanicRecovered    *bool   `json:"panic_recovered,omitempty"`
	ResponseCommitted *bool   `json:"response_committed,omitempty"`
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
	allowed := map[string]bool{"from": true, "to": true, "service": true, "module": true, "level": true, "message": true, "request_id": true, "event_id": true, "error_code": true, "limit": true, "cursor": true}
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
	options.Filters.EventID = single(values, "event_id")
	options.Filters.ErrorCode = single(values, "error_code")
	if !validLogVocabulary(options.Filters.Service, options.Filters.Module, options.Filters.Message) {
		return Options{}, validation()
	}
	if options.Filters.Level != "" && options.Filters.Level != "info" && options.Filters.Level != "warn" && options.Filters.Level != "error" {
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
	return options, nil
}
func validLogVocabulary(service, module, message string) bool {
	if service == "" && module == "" && message == "" {
		return true
	}
	for candidateService, modules := range logVocabulary {
		if service != "" && service != candidateService {
			continue
		}
		for candidateModule, messages := range modules {
			if module != "" && module != candidateModule {
				continue
			}
			if message == "" {
				return true
			}
			if _, ok := messages[message]; ok {
				return true
			}
		}
	}
	return false
}

func validErrorCode(value string) bool {
	switch apperror.Code(value) {
	case apperror.CodeValidationFailed, apperror.CodeAuthenticationRequired, apperror.CodePermissionDenied,
		apperror.CodeInvalidCredentials, apperror.CodeUsernameConflict, apperror.CodePostNotFound,
		apperror.CodeNotificationNotFound, apperror.CodeSearchUnavailable, apperror.CodeLogsUnavailable,
		apperror.CodePluginPackageInvalid, apperror.CodePluginNotFound, apperror.CodePluginConflict,
		apperror.CodePluginOperationInProgress, apperror.CodePluginOperationFailed,
		apperror.CodeMonitorUnavailable, apperror.CodeInternal:
		return true
	default:
		return false
	}
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
	defer response.Body.Close()
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
	for field, value := range map[string]string{"service": filters.Service, "module": filters.Module, "level": filters.Level, "message": filters.Message, "request_id": filters.RequestID, "event_id": filters.EventID, "error_code": filters.ErrorCode} {
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
	defer response.Body.Close()
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
func (r *ElasticsearchRepository) ClosePointInTime(ctx context.Context, pit string) error {
	encoded, _ := json.Marshal(map[string]string{"id": pit})
	response, err := r.do(ctx, http.MethodDelete, "/_pit", bytes.NewReader(encoded))
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrUnavailable
	}
	return nil
}
func (r *ElasticsearchRepository) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, path, body)
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
		Entry
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil || raw.Timestamp == "" || raw.LogSchemaVersion != 1 {
		return Entry{}, errors.New("invalid log document")
	}
	raw.Entry.Timestamp = raw.Timestamp
	return raw.Entry, nil
}
