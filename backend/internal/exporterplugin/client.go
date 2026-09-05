package exporterplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

const (
	MaxPackageBytes  int64 = 64 << 20
	maxResponseBytes       = 1 << 20
)

var stableSemVer = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type Client struct {
	baseURL string
	token   string
	client  *http.Client
}

type SafeError struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

type Status struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Version       string     `json:"version"`
	Kind          string     `json:"kind"`
	Source        string     `json:"source"`
	DesiredState  string     `json:"desired_state"`
	ObservedState string     `json:"observed_state"`
	InstalledAt   time.Time  `json:"installed_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	StartedAt     *time.Time `json:"started_at"`
	LastScrapeAt  *time.Time `json:"last_scrape_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     *SafeError `json:"last_error,omitempty"`
}

type wireStatus struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Kind          string          `json:"kind"`
	Source        string          `json:"source"`
	DesiredState  string          `json:"desired_state"`
	ObservedState string          `json:"observed_state"`
	InstalledAt   string          `json:"installed_at"`
	UpdatedAt     string          `json:"updated_at"`
	StartedAt     *string         `json:"started_at"`
	LastScrapeAt  *string         `json:"last_scrape_at"`
	LastSuccessAt *string         `json:"last_success_at"`
	LastError     json.RawMessage `json:"last_error,omitempty"`
}

type wireSafeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	At      string `json:"at"`
}

var safeErrorMessages = map[string]map[string]bool{
	"start_failed":     {"plugin failed to start": true},
	"stop_failed":      {"plugin failed to stop": true, "plugin process ownership could not be verified": true},
	"update_failed":    {"plugin update failed and was rolled back": true},
	"rollback_failed":  {"plugin update rollback requires repair": true, "plugin update rollback could not restart the previous version": true},
	"recovery_invalid": {"plugin installation requires repair": true},
	"recovery_failed":  {"plugin failed to recover": true},
	"process_exited":   {"plugin process exited unexpectedly": true},
}

func NewClient(baseURL, token string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid monitor URL")
	}
	if len(token) < 32 {
		return nil, errors.New("monitor token must contain at least 32 bytes")
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

func (c *Client) List(ctx context.Context) ([]Status, error) {
	body, _, err := c.request(ctx, http.MethodGet, "/internal/v1/exporter-plugins", nil, "", http.StatusOK)
	if err != nil {
		return nil, err
	}
	items, err := decodeStatusList(body)
	if err != nil {
		return nil, monitorUnavailable()
	}
	return items, nil
}

func (c *Client) Get(ctx context.Context, id string) (Status, error) {
	return c.statusRequest(ctx, http.MethodGet, "/internal/v1/exporter-plugins/"+url.PathEscape(id), nil, "", http.StatusOK)
}
func (c *Client) Start(ctx context.Context, id string) (Status, error) {
	return c.action(ctx, id, "start")
}
func (c *Client) Stop(ctx context.Context, id string) (Status, error) {
	return c.action(ctx, id, "stop")
}
func (c *Client) action(ctx context.Context, id, action string) (Status, error) {
	return c.statusRequest(ctx, http.MethodPost, "/internal/v1/exporter-plugins/"+url.PathEscape(id)+"/"+action, http.NoBody, "", http.StatusOK)
}
func (c *Client) Upload(ctx context.Context, path string, expectedStatus int, boundaryWriter func(*multipart.Writer) error) (Status, error) {
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	contentType := multipartWriter.FormDataContentType()
	go func() {
		err := boundaryWriter(multipartWriter)
		if closeErr := multipartWriter.Close(); err == nil {
			err = closeErr
		}
		_ = writer.CloseWithError(err)
	}()
	return c.statusRequest(ctx, http.MethodPost, path, reader, contentType, expectedStatus)
}
func (c *Client) statusRequest(ctx context.Context, method, path string, body io.Reader, contentType string, expectedStatus int) (Status, error) {
	payload, _, err := c.request(ctx, method, path, body, contentType, expectedStatus)
	if err != nil {
		return Status{}, err
	}
	item, err := decodeStatus(payload)
	if err != nil {
		return Status{}, monitorUnavailable()
	}
	return item, nil
}
func (c *Client) request(ctx context.Context, method, path string, body io.Reader, contentType string, expectedStatus int) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, 0, monitorUnavailable()
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, 0, monitorUnavailable()
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(payload) > maxResponseBytes {
		return nil, response.StatusCode, monitorUnavailable()
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code, decodeErr := decodeErrorCode(payload)
		if decodeErr != nil {
			return nil, response.StatusCode, monitorUnavailable()
		}
		return nil, response.StatusCode, mapMonitorError(code)
	}
	if response.StatusCode != expectedStatus {
		return nil, response.StatusCode, monitorUnavailable()
	}
	return payload, response.StatusCode, nil
}

func decodeStatusList(payload []byte) ([]Status, error) {
	data, err := envelopeData(payload)
	if err != nil || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, errors.New("invalid list envelope")
	}
	var rawItems []json.RawMessage
	if err := decodeStrict(data, &rawItems); err != nil || rawItems == nil || len(rawItems) > 1 {
		return nil, errors.New("invalid status list")
	}
	items := make([]Status, 0, len(rawItems))
	for _, raw := range rawItems {
		item, err := decodeStatusObject(raw)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}
func decodeStatus(payload []byte) (Status, error) {
	data, err := envelopeData(payload)
	if err != nil {
		return Status{}, err
	}
	return decodeStatusObject(data)
}
func envelopeData(payload []byte) (json.RawMessage, error) {
	if err := validateJSON(payload); err != nil {
		return nil, err
	}
	var envelope map[string]json.RawMessage
	if err := decodeStrict(payload, &envelope); err != nil || !exactKeys(envelope, "data") {
		return nil, errors.New("invalid data envelope")
	}
	return envelope["data"], nil
}
func decodeStatusObject(payload []byte) (Status, error) {
	if bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		return Status{}, errors.New("null status")
	}
	var fields map[string]json.RawMessage
	if err := decodeStrict(payload, &fields); err != nil {
		return Status{}, err
	}
	required := []string{"id", "name", "version", "kind", "source", "desired_state", "observed_state", "installed_at", "updated_at", "started_at", "last_scrape_at", "last_success_at"}
	if !(exactKeys(fields, required...) || exactKeys(fields, append(required, "last_error")...)) {
		return Status{}, errors.New("invalid status fields")
	}
	if raw, ok := fields["last_error"]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return Status{}, errors.New("null last error")
	}
	var wire wireStatus
	if err := decodeStrict(payload, &wire); err != nil {
		return Status{}, err
	}
	installedAt, err := parseUTC(wire.InstalledAt)
	if err != nil {
		return Status{}, err
	}
	updatedAt, err := parseUTC(wire.UpdatedAt)
	if err != nil {
		return Status{}, err
	}
	item := Status{ID: wire.ID, Name: wire.Name, Version: wire.Version, Kind: wire.Kind, Source: wire.Source, DesiredState: wire.DesiredState, ObservedState: wire.ObservedState, InstalledAt: installedAt, UpdatedAt: updatedAt}
	if item.StartedAt, err = parseOptionalUTC(wire.StartedAt); err != nil {
		return Status{}, err
	}
	if item.LastScrapeAt, err = parseOptionalUTC(wire.LastScrapeAt); err != nil {
		return Status{}, err
	}
	if item.LastSuccessAt, err = parseOptionalUTC(wire.LastSuccessAt); err != nil {
		return Status{}, err
	}
	if len(wire.LastError) > 0 {
		var errorFields map[string]json.RawMessage
		if err := decodeStrict(wire.LastError, &errorFields); err != nil || !exactKeys(errorFields, "code", "message", "at") {
			return Status{}, errors.New("invalid last error")
		}
		var safe wireSafeError
		if err := decodeStrict(wire.LastError, &safe); err != nil {
			return Status{}, err
		}
		at, parseErr := parseUTC(safe.At)
		if parseErr != nil || !safeErrorMessages[safe.Code][safe.Message] {
			return Status{}, errors.New("unsafe last error")
		}
		item.LastError = &SafeError{Code: safe.Code, Message: safe.Message, At: at}
	}
	if err := validateStatus(item); err != nil {
		return Status{}, err
	}
	return item, nil
}
func validateStatus(item Status) error {
	if item.ID != "redis-exporter" || item.Kind != "metrics-exporter" || item.Source != "redis" || !stableSemVer.MatchString(item.Version) || !safeText(item.Name, 1, 80) {
		return errors.New("invalid plugin identity")
	}
	if item.DesiredState != "running" && item.DesiredState != "stopped" {
		return errors.New("invalid desired state")
	}
	validObserved := map[string]bool{"installing": true, "starting": true, "running": true, "stopping": true, "stopped": true, "updating": true, "failed": true}
	if !validObserved[item.ObservedState] || item.UpdatedAt.Before(item.InstalledAt) {
		return errors.New("invalid plugin state")
	}
	if (item.ObservedState == "running" || item.ObservedState == "starting") && item.DesiredState != "running" {
		return errors.New("invalid running state")
	}
	if item.ObservedState == "stopping" && item.DesiredState != "stopped" {
		return errors.New("invalid stopping state")
	}
	if item.ObservedState == "running" && item.StartedAt == nil {
		return errors.New("running plugin has no start time")
	}
	for _, value := range []*time.Time{item.StartedAt, item.LastScrapeAt, item.LastSuccessAt} {
		if value != nil && value.Before(item.InstalledAt) {
			return errors.New("timestamp predates installation")
		}
	}
	if item.LastSuccessAt != nil && (item.LastScrapeAt == nil || item.LastSuccessAt.After(*item.LastScrapeAt)) {
		return errors.New("invalid scrape timestamps")
	}
	if item.LastError != nil && item.LastError.At.Before(item.InstalledAt) {
		return errors.New("invalid error timestamp")
	}
	return nil
}
func parseOptionalUTC(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := parseUTC(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
func parseUTC(value string) (time.Time, error) {
	if value == "" || !strings.HasSuffix(value, "Z") {
		return time.Time{}, errors.New("timestamp is not UTC")
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Format(time.RFC3339Nano) != value {
		return time.Time{}, errors.New("invalid timestamp")
	}
	return parsed.UTC(), nil
}
func safeText(value string, min, max int) bool {
	length := utf8.RuneCountInString(value)
	if length < min || length > max || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return strings.TrimSpace(value) == value
}
func decodeErrorCode(payload []byte) (string, error) {
	if err := validateJSON(payload); err != nil {
		return "", err
	}
	var envelope map[string]json.RawMessage
	if err := decodeStrict(payload, &envelope); err != nil || !exactKeys(envelope, "error") {
		return "", errors.New("invalid error envelope")
	}
	var body map[string]json.RawMessage
	if err := decodeStrict(envelope["error"], &body); err != nil || !exactKeys(body, "code", "message") {
		return "", errors.New("invalid error body")
	}
	var code, message string
	if err := decodeStrict(body["code"], &code); err != nil {
		return "", err
	}
	if err := decodeStrict(body["message"], &message); err != nil || !safeText(message, 1, 160) {
		return "", errors.New("invalid error message")
	}
	return code, nil
}
func validateJSON(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return errors.New("trailing JSON content")
	}
	return nil
}
func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return errors.New("duplicate or invalid object key")
			}
			seen[key] = true
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("invalid object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("invalid array")
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delim)
	}
	return nil
}
func decodeStrict(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing JSON content")
	}
	return nil
}
func exactKeys[T any](values map[string]T, keys ...string) bool {
	if len(values) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}
func mapMonitorError(code string) error {
	switch code {
	case "plugin_package_invalid":
		return apperror.New(apperror.CodePluginPackageInvalid, "plugin package is invalid")
	case "plugin_not_found":
		return apperror.New(apperror.CodePluginNotFound, "plugin was not found")
	case "plugin_conflict":
		return apperror.New(apperror.CodePluginConflict, "plugin conflicts with the installed state")
	case "plugin_operation_in_progress":
		return apperror.New(apperror.CodePluginOperationInProgress, "plugin operation is already in progress")
	case "plugin_operation_failed":
		return apperror.New(apperror.CodePluginOperationFailed, "plugin operation failed")
	default:
		return monitorUnavailable()
	}
}
func monitorUnavailable() error {
	return apperror.New(apperror.CodeMonitorUnavailable, "monitor service is unavailable")
}
