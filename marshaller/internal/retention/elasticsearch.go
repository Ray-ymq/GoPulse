package retention

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/componentmetrics"
)

const responseLimit = 64 << 10

type Index struct {
	Name      string `json:"index"`
	DocsCount int64  `json:"docs.count"`
	StoreSize int64  `json:"store.size"`
}

type HTTPError struct {
	Status int
	Op     string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("elasticsearch %s returned HTTP %d", e.Op, e.Status)
}

func IsNotFound(err error) bool {
	var target *HTTPError
	return errors.As(err, &target) && target.Status == http.StatusNotFound
}

func IsPermissionDenied(err error) bool {
	var target *HTTPError
	return errors.As(err, &target) && (target.Status == http.StatusForbidden || target.Status == http.StatusUnauthorized)
}

func IsTransient(err error) bool {
	var target *HTTPError
	if errors.As(err, &target) {
		return target.Status == http.StatusRequestTimeout || target.Status == http.StatusTooManyRequests || target.Status >= 500
	}
	return err != nil
}

type Ownership struct {
	Valid       bool
	ClusterUUID string
	Alias       bool
	Mapping     bool
	Marker      bool
	Reason      string
}

type Store interface {
	ClusterIdentity(context.Context) (string, error)
	List(context.Context) ([]Index, error)
	Inspect(context.Context, string, Policy, string) (Ownership, error)
	BlockWrites(context.Context, string) error
	DeleteIndex(context.Context, string) error
}

type Elasticsearch struct {
	baseURL string
	client  *http.Client
	now     func() time.Time
}

func NewElasticsearch(baseURL string, timeout time.Duration, container bool) (*Elasticsearch, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid retention Elasticsearch URL")
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if timeout <= 0 {
		return nil, errors.New("retention Elasticsearch timeout must be positive")
	}
	if (!container && (ip == nil || !ip.IsLoopback())) || (container && (ip != nil || host == "localhost" || host == "host.docker.internal" || !validServiceDNSName(host))) {
		return nil, errors.New("retention Elasticsearch origin is outside the allowed mode")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = timeout
	return &Elasticsearch{baseURL: strings.TrimRight(baseURL, "/"), now: time.Now, client: &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func validServiceDNSName(host string) bool {
	if host == "" || len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
				return false
			}
		}
	}
	return true
}

func (e *Elasticsearch) ClusterIdentity(ctx context.Context) (string, error) {
	status, body, err := e.do(ctx, http.MethodGet, "/", nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", &HTTPError{Status: status, Op: "cluster identity"}
	}
	var response struct {
		ClusterUUID string `json:"cluster_uuid"`
	}
	if json.Unmarshal(body, &response) != nil || response.ClusterUUID == "" || len(response.ClusterUUID) > 256 {
		return "", errors.New("Elasticsearch cluster identity is missing")
	}
	return response.ClusterUUID, nil
}

func (e *Elasticsearch) List(ctx context.Context) ([]Index, error) {
	status, body, err := e.do(ctx, http.MethodGet, "/_cat/indices?format=json&h=index,docs.count,store.size", nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, &HTTPError{Status: status, Op: "list indices"}
	}
	var raw []struct {
		Name      string `json:"index"`
		DocsCount string `json:"docs.count"`
		StoreSize string `json:"store.size"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return nil, errors.New("Elasticsearch index inventory is invalid")
	}
	result := make([]Index, 0, len(raw))
	for _, item := range raw {
		if item.Name == "" {
			continue
		}
		docs, _ := strconv.ParseInt(strings.TrimSpace(item.DocsCount), 10, 64)
		store, err := parseSize(item.StoreSize)
		if err != nil {
			store = -1
		}
		result = append(result, Index{Name: item.Name, DocsCount: docs, StoreSize: store})
	}
	return result, nil
}

func parseSize(raw string) (int64, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || raw == "-" {
		return 0, nil
	}
	units := []struct {
		suffix string
		value  float64
	}{
		{"pb", 1 << 50}, {"tb", 1 << 40}, {"gb", 1 << 30}, {"mb", 1 << 20}, {"kb", 1 << 10}, {"b", 1},
	}
	for _, unit := range units {
		if strings.HasSuffix(raw, unit.suffix) {
			value, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(raw, unit.suffix)), 64)
			if err != nil || value < 0 || value > float64(^uint64(0)>>1)/unit.value {
				return 0, errors.New("invalid store size")
			}
			return int64(value * unit.value), nil
		}
	}
	return 0, errors.New("unknown store size")
}

func (e *Elasticsearch) Inspect(ctx context.Context, index string, policy Policy, expectedCluster string) (Ownership, error) {
	identity, err := e.ClusterIdentity(ctx)
	if err != nil {
		return Ownership{}, err
	}
	ownership := Ownership{ClusterUUID: identity}
	if expectedCluster == "" || identity != expectedCluster {
		ownership.Reason = "cluster_identity_mismatch"
		return ownership, nil
	}
	mappingStatus, mappingBody, err := e.do(ctx, http.MethodGet, "/"+url.PathEscape(index)+"/_mapping", nil)
	if err != nil {
		return Ownership{}, err
	}
	if mappingStatus == http.StatusNotFound {
		ownership.Reason = "mapping_missing"
		return ownership, nil
	}
	if mappingStatus < 200 || mappingStatus >= 300 {
		return Ownership{}, &HTTPError{Status: mappingStatus, Op: "inspect mapping"}
	}
	ownership.Mapping, ownership.Marker = validMappingMarker(mappingBody, index, policy)
	aliasStatus, aliasBody, err := e.do(ctx, http.MethodGet, "/"+url.PathEscape(index)+"/_alias/"+url.PathEscape(policy.Alias), nil)
	if err != nil {
		return Ownership{}, err
	}
	if aliasStatus == http.StatusNotFound {
		ownership.Reason = "alias_missing"
		return ownership, nil
	}
	if aliasStatus < 200 || aliasStatus >= 300 {
		return Ownership{}, &HTTPError{Status: aliasStatus, Op: "inspect alias"}
	}
	ownership.Alias = validAlias(aliasBody, index, policy.Alias)
	if !ownership.Mapping || !ownership.Marker || !ownership.Alias {
		ownership.Reason = "ownership_contract_mismatch"
		return ownership, nil
	}
	ownership.Valid = true
	return ownership, nil
}

func validMappingMarker(body []byte, index string, policy Policy) (mapping, marker bool) {
	var response map[string]struct {
		Mappings struct {
			Dynamic    string         `json:"dynamic"`
			Meta       map[string]any `json:"_meta"`
			Properties map[string]any `json:"properties"`
		} `json:"mappings"`
	}
	if json.Unmarshal(body, &response) != nil || len(response) != 1 {
		return false, false
	}
	entry, ok := response[index]
	if !ok || entry.Mappings.Dynamic != "strict" || !validMappingProperties(entry.Mappings.Properties, policy.Stream) {
		return false, false
	}
	marked := entry.Mappings.Meta["gopulse_product"] == "gopulse-observability" && entry.Mappings.Meta["gopulse_stream"] == string(policy.Stream) && entry.Mappings.Meta["gopulse_schema"] == "v1"
	if !marked && !policy.LegacyIndices[index] {
		return true, false
	}
	return true, marked || policy.LegacyIndices[index]
}

func validMappingProperties(properties map[string]any, stream Stream) bool {
	expected := map[string]bool{}
	if stream == Logs {
		for _, name := range []string{"version", "revision", "event", "runtime_contract_version", "runtime_mode", "listen", "@timestamp", "log_schema_version", "level", "service", "instance_id", "module", "message", "request_id", "trace_id", "span_id", "attempt_id", "event_id", "event_type", "user_id", "post_id", "content_revision", "comment_id", "notification_id", "outbox_id", "method", "route", "status", "duration_ms", "response_bytes", "error_code", "reason", "operation", "resource", "stage", "result", "attempt", "batch_size", "document_count", "panic_recovered", "response_committed"} {
			expected[name] = true
		}
	} else if stream == Events {
		for _, name := range []string{"@timestamp", "event_schema_version", "event_name", "source", "severity", "message", "metadata"} {
			expected[name] = true
		}
	} else {
		return false
	}
	if len(properties) != len(expected) {
		return false
	}
	for name := range expected {
		if _, ok := properties[name]; !ok {
			return false
		}
	}
	if stream == Events {
		metadata, ok := properties["metadata"].(map[string]any)
		if !ok {
			return false
		}
		if dynamic, ok := metadata["dynamic"].(string); ok && dynamic != "strict" {
			return false
		}
		props, ok := metadata["properties"].(map[string]any)
		if !ok || len(props) != 8 {
			return false
		}
		for _, name := range []string{"plugin_id", "plugin_version", "previous_plugin_version", "operation", "from_state", "to_state", "error_code", "scrape_status"} {
			if _, ok := props[name]; !ok {
				return false
			}
		}
	}
	return true
}

func validAlias(body []byte, index, alias string) bool {
	var response map[string]struct {
		Aliases map[string]json.RawMessage `json:"aliases"`
	}
	if json.Unmarshal(body, &response) != nil || len(response) != 1 {
		return false
	}
	entry, ok := response[index]
	if !ok {
		return false
	}
	_, ok = entry.Aliases[alias]
	return ok
}

func (e *Elasticsearch) BlockWrites(ctx context.Context, index string) error {
	status, _, err := e.do(ctx, http.MethodPut, "/"+url.PathEscape(index)+"/_settings", bytes.NewReader([]byte(`{"index":{"blocks.write":true}}`)))
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &HTTPError{Status: status, Op: "block index writes"}
	}
	return nil
}

func (e *Elasticsearch) DeleteIndex(ctx context.Context, index string) error {
	status, _, err := e.do(ctx, http.MethodDelete, "/"+url.PathEscape(index), nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return &HTTPError{Status: status, Op: "delete index"}
	}
	if status < 200 || status >= 300 {
		return &HTTPError{Status: status, Op: "delete index"}
	}
	return nil
}

func (e *Elasticsearch) do(ctx context.Context, method, path string, body io.Reader) (int, []byte, error) {
	req, err := componentmetrics.NewRequest(ctx, method, e.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := e.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if readErr != nil || len(data) > responseLimit {
		return response.StatusCode, nil, errors.New("Elasticsearch response exceeded limit")
	}
	return response.StatusCode, data, nil
}
