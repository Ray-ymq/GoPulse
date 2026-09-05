package eventquery

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
	"unicode"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

const (
	ReadAlias          = "gopulse-events-v1-read"
	DefaultLimit       = 50
	MaximumLimit       = 100
	pitKeepAlive       = "2m"
	maximumCursorBytes = 8192
)

var semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

var ErrUnavailable = errors.New("events unavailable")
var ErrAliasMissing = errors.New("event alias missing")
var ErrPITExpired = errors.New("event PIT expired")

type Filters struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Source    string `json:"source,omitempty"`
	EventName string `json:"event_name,omitempty"`
	Severity  string `json:"severity,omitempty"`
	PluginID  string `json:"plugin_id,omitempty"`
	Operation string `json:"operation,omitempty"`
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

type Metadata struct {
	PluginID              string `json:"plugin_id"`
	PluginVersion         string `json:"plugin_version,omitempty"`
	PreviousPluginVersion string `json:"previous_plugin_version,omitempty"`
	Operation             string `json:"operation,omitempty"`
	FromState             string `json:"from_state,omitempty"`
	ToState               string `json:"to_state,omitempty"`
	ErrorCode             string `json:"error_code,omitempty"`
	ScrapeStatus          string `json:"scrape_status,omitempty"`
}
type Entry struct {
	Timestamp string   `json:"timestamp"`
	EventName string   `json:"event_name"`
	Source    string   `json:"source"`
	Severity  string   `json:"severity"`
	Message   string   `json:"message"`
	Metadata  Metadata `json:"metadata"`
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
	allowed := map[string]bool{"from": true, "to": true, "source": true, "event_name": true, "severity": true, "plugin_id": true, "operation": true, "error_code": true, "limit": true, "cursor": true}
	for key, list := range values {
		if !allowed[key] || len(list) != 1 || list[0] == "" || !utf8.ValidString(list[0]) || len(list[0]) > 256 || hasControl(list[0]) {
			return Options{}, validation()
		}
	}
	if cursor, ok := values["cursor"]; ok {
		if len(values) != 1 || len(cursor[0]) > maximumCursorBytes {
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
	options.Filters.Source = single(values, "source")
	options.Filters.EventName = single(values, "event_name")
	options.Filters.Severity = single(values, "severity")
	options.Filters.PluginID = single(values, "plugin_id")
	options.Filters.Operation = single(values, "operation")
	options.Filters.ErrorCode = single(values, "error_code")
	if !validFilters(options.Filters) {
		return Options{}, validation()
	}
	return options, nil
}

func validFilters(filters Filters) bool {
	if filters.Source != "" && filters.Source != "monitor" {
		return false
	}
	if filters.PluginID != "" && filters.PluginID != "redis-exporter" {
		return false
	}
	severities := map[string]string{
		"exporter_plugin_installed": "info", "exporter_plugin_started": "info", "exporter_plugin_stopped": "info", "exporter_plugin_updated": "info",
		"exporter_plugin_failed": "error", "exporter_plugin_exited": "error", "metrics_collection_failed": "warn", "metrics_collection_recovered": "info",
		"metrics_target_unavailable": "warn", "metrics_target_recovered": "info",
	}
	if filters.Severity != "" && filters.Severity != "info" && filters.Severity != "warn" && filters.Severity != "error" {
		return false
	}
	if filters.EventName != "" {
		severity, ok := severities[filters.EventName]
		if !ok || (filters.Severity != "" && filters.Severity != severity) {
			return false
		}
	}
	operations := map[string]bool{"install": true, "start": true, "stop": true, "update": true, "recover": true, "scrape": true, "publish": true}
	if filters.Operation != "" && !operations[filters.Operation] {
		return false
	}
	if filters.EventName != "" && filters.Operation != "" && !eventOperation(filters.EventName, filters.Operation) {
		return false
	}
	if filters.ErrorCode != "" {
		if !knownErrorCode(filters.ErrorCode) || (filters.EventName != "" && filters.EventName != "exporter_plugin_failed" && filters.EventName != "exporter_plugin_exited" && filters.EventName != "metrics_collection_failed") {
			return false
		}
		if filters.Operation != "" && !operationError(filters.Operation, filters.ErrorCode) {
			return false
		}
		if filters.EventName == "exporter_plugin_exited" && filters.ErrorCode != "process_exited" {
			return false
		}
		if filters.EventName == "metrics_collection_failed" && filters.ErrorCode == "process_exited" {
			return false
		}
	}
	return true
}

func eventOperation(name, operation string) bool {
	allowed := map[string]map[string]bool{
		"exporter_plugin_installed": {"install": true}, "exporter_plugin_started": {"start": true}, "exporter_plugin_stopped": {"stop": true}, "exporter_plugin_updated": {"update": true},
		"exporter_plugin_failed": {"start": true, "stop": true, "update": true, "recover": true}, "exporter_plugin_exited": {"start": true},
		"metrics_collection_failed": {"scrape": true, "publish": true}, "metrics_collection_recovered": {"scrape": true}, "metrics_target_unavailable": {"scrape": true}, "metrics_target_recovered": {"scrape": true},
	}
	return allowed[name][operation]
}

func knownErrorCode(code string) bool {
	return map[string]bool{
		"start_failed": true, "stop_failed": true, "update_failed": true, "rollback_failed": true, "recovery_failed": true, "recovery_invalid": true, "process_exited": true,
		"scrape_timeout": true, "network_failed": true, "response_too_large": true, "parse_failed": true, "contract_invalid": true, "content_invalid": true, "http_invalid": true, "scrape_failed": true, "message_id_failed": true, "publish_failed": true,
	}[code]
}

func operationError(operation, code string) bool {
	allowed := map[string]map[string]bool{
		"start": {"start_failed": true, "process_exited": true}, "stop": {"stop_failed": true}, "update": {"update_failed": true, "rollback_failed": true}, "recover": {"recovery_failed": true, "recovery_invalid": true},
		"scrape":  {"scrape_timeout": true, "network_failed": true, "response_too_large": true, "parse_failed": true, "contract_invalid": true, "content_invalid": true, "http_invalid": true, "scrape_failed": true, "message_id_failed": true},
		"publish": {"publish_failed": true},
	}
	return allowed[operation][code]
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
	payload.PIT = result.PIT
	payload.After = result.Hits[payload.Limit-1].Sort
	payload.ExpiresAt = s.now().Add(2 * time.Minute).Unix()
	cursor, err := s.encode(payload)
	if err != nil {
		return Page{}, unavailable()
	}
	return Page{Entries: entries, NextCursor: &cursor}, nil
}

func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte("gopulse/backend/event-query-cursor/v1\x00" + secret))
	return sum[:]
}
func (s *Service) encode(payload cursorPayload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(append(body, mac.Sum(nil)...)), nil
}
func (s *Service) decode(value string) (cursorPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) <= sha256.Size || len(raw) > maximumCursorBytes {
		return cursorPayload{}, errors.New("invalid cursor")
	}
	body, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return cursorPayload{}, errors.New("invalid cursor")
	}
	var payload cursorPayload
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || payload.Version != 1 || payload.PIT == "" || len(payload.PIT) > 4096 || payload.Limit < 1 || payload.Limit > MaximumLimit || payload.ExpiresAt < s.now().Unix() || !validFilters(payload.Filters) {
		return cursorPayload{}, errors.New("invalid cursor")
	}
	return payload, nil
}

func validation() error {
	return apperror.New(apperror.CodeValidationFailed, "event query parameters are invalid")
}
func unavailable() error {
	return apperror.New(apperror.CodeEventsUnavailable, "events are temporarily unavailable")
}

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
	if pit == "" || len(pit) > 4096 || limit < 1 || limit > MaximumLimit || !validFilters(filters) {
		return SearchResult{}, ErrUnavailable
	}
	must := []any{map[string]any{"range": map[string]any{"@timestamp": map[string]string{"gte": filters.From, "lt": filters.To}}}}
	for field, value := range map[string]string{"source": filters.Source, "event_name": filters.EventName, "severity": filters.Severity, "metadata.plugin_id": filters.PluginID, "metadata.operation": filters.Operation, "metadata.error_code": filters.ErrorCode} {
		if value != "" {
			must = append(must, map[string]any{"term": map[string]string{field: value}})
		}
	}
	body := map[string]any{"size": limit + 1, "_source": []string{"@timestamp", "event_schema_version", "event_name", "source", "severity", "message", "metadata"}, "pit": map[string]string{"id": pit, "keep_alive": pitKeepAlive}, "query": map[string]any{"bool": map[string]any{"filter": must}}, "sort": []any{map[string]any{"@timestamp": map[string]string{"order": "desc", "format": "strict_date_optional_time_nanos"}}, map[string]any{"_shard_doc": map[string]string{"order": "desc"}}}}
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
				Index  string            `json:"_index"`
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
		if len(raw.Sort) != 2 || len(raw.Index) < len("gopulse-events-v1-") || raw.Index[:len("gopulse-events-v1-")] != "gopulse-events-v1-" {
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
	payload, err := io.ReadAll(io.LimitReader(body, (1<<20)+1))
	if err != nil || len(payload) > 1<<20 {
		return errors.New("invalid Elasticsearch response")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(destination); err != nil {
		return errors.New("invalid Elasticsearch response")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid Elasticsearch response")
	}
	return nil
}
func decodeEntry(source []byte) (Entry, error) {
	var raw struct {
		Timestamp          string   `json:"@timestamp"`
		EventSchemaVersion int      `json:"event_schema_version"`
		EventName          string   `json:"event_name"`
		Source             string   `json:"source"`
		Severity           string   `json:"severity"`
		Message            string   `json:"message"`
		Metadata           Metadata `json:"metadata"`
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&raw) != nil || raw.EventSchemaVersion != 1 || !validDocument(raw.EventName, raw.Source, raw.Severity, raw.Message, raw.Metadata) {
		return Entry{}, errors.New("invalid event document")
	}
	at, err := time.Parse(time.RFC3339Nano, raw.Timestamp)
	if err != nil || at.Location() != time.UTC {
		return Entry{}, errors.New("invalid event timestamp")
	}
	return Entry{Timestamp: raw.Timestamp, EventName: raw.EventName, Source: raw.Source, Severity: raw.Severity, Message: raw.Message, Metadata: raw.Metadata}, nil
}
func validDocument(name, source, severity, message string, m Metadata) bool {
	messages := map[string]string{
		"exporter_plugin_installed": "exporter plugin installed", "exporter_plugin_started": "exporter plugin started", "exporter_plugin_stopped": "exporter plugin stopped", "exporter_plugin_updated": "exporter plugin updated",
		"exporter_plugin_failed": "exporter plugin operation failed", "exporter_plugin_exited": "exporter plugin exited unexpectedly", "metrics_collection_failed": "metrics collection failed", "metrics_collection_recovered": "metrics collection recovered",
		"metrics_target_unavailable": "metrics target unavailable", "metrics_target_recovered": "metrics target recovered",
	}
	severities := map[string]string{
		"exporter_plugin_installed": "info", "exporter_plugin_started": "info", "exporter_plugin_stopped": "info", "exporter_plugin_updated": "info", "exporter_plugin_failed": "error", "exporter_plugin_exited": "error",
		"metrics_collection_failed": "warn", "metrics_collection_recovered": "info", "metrics_target_unavailable": "warn", "metrics_target_recovered": "info",
	}
	if source != "monitor" || messages[name] != message || severities[name] != severity || m.PluginID != "redis-exporter" {
		return false
	}
	empty := m.ErrorCode == "" && m.ScrapeStatus == ""
	switch name {
	case "exporter_plugin_installed":
		return empty && semverPattern.MatchString(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "install" && m.FromState == "not_installed" && m.ToState == "running"
	case "exporter_plugin_started":
		return empty && semverPattern.MatchString(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "start" && (m.FromState == "stopped" || m.FromState == "failed") && m.ToState == "running"
	case "exporter_plugin_stopped":
		return empty && semverPattern.MatchString(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "stop" && (m.FromState == "running" || m.FromState == "failed") && m.ToState == "stopped"
	case "exporter_plugin_updated":
		return empty && semverPattern.MatchString(m.PluginVersion) && semverPattern.MatchString(m.PreviousPluginVersion) && m.Operation == "update" && (m.FromState == "running" || m.FromState == "stopped") && m.FromState == m.ToState
	case "exporter_plugin_failed":
		return semverPattern.MatchString(m.PluginVersion) && m.PreviousPluginVersion == "" && m.FromState == "" && validEventState(m.ToState) && operationError(m.Operation, m.ErrorCode) && m.ErrorCode != "process_exited" && m.ScrapeStatus == ""
	case "exporter_plugin_exited":
		return semverPattern.MatchString(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "start" && m.FromState == "" && m.ToState == "failed" && m.ErrorCode == "process_exited" && m.ScrapeStatus == ""
	case "metrics_collection_failed":
		return m.PluginVersion == "" && m.PreviousPluginVersion == "" && m.FromState == "" && m.ToState == "" && operationError(m.Operation, m.ErrorCode) && m.ScrapeStatus == ""
	case "metrics_collection_recovered", "metrics_target_recovered":
		return metricsDocument(m, "success")
	case "metrics_target_unavailable":
		return metricsDocument(m, "target_unavailable")
	}
	return false
}

func validEventState(value string) bool {
	return value == "not_installed" || value == "stopped" || value == "running" || value == "failed"
}
func metricsDocument(m Metadata, status string) bool {
	return m.PluginVersion == "" && m.PreviousPluginVersion == "" && m.Operation == "scrape" && m.FromState == "" && m.ToState == "" && m.ErrorCode == "" && m.ScrapeStatus == status
}

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
