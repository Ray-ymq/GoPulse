package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	logtransform "github.com/Ray-ymq/GoPulse/marshaller/internal/logs"
)

const (
	TemplateName  = "gopulse-logs-v1-template"
	IndexPrefix   = "gopulse-logs-v1-"
	ReadAlias     = "gopulse-logs-v1-read"
	responseLimit = 16 << 10
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var datePattern = regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}$`)

var requiredPropertyTypes = map[string]string{
	"@timestamp": "date_nanos", "log_schema_version": "integer", "level": "keyword",
	"service": "keyword", "module": "keyword", "message": "keyword", "request_id": "keyword",
	"event_id": "keyword", "event_type": "keyword", "user_id": "long", "post_id": "long",
	"comment_id": "long", "notification_id": "long", "outbox_id": "long", "method": "keyword",
	"route": "keyword", "status": "long", "duration_ms": "long", "response_bytes": "long",
	"error_code": "keyword", "reason": "keyword", "operation": "keyword", "resource": "keyword",
	"stage": "keyword", "result": "keyword", "attempt": "long", "batch_size": "long",
	"document_count": "long", "panic_recovered": "boolean", "response_committed": "boolean",
}

type Client struct {
	baseURL string
	client  *http.Client
	mu      sync.Mutex
}

func New(baseURL string, timeout time.Duration) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid Elasticsearch URL")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || timeout <= 0 {
		return nil, errors.New("Elasticsearch must use a loopback HTTP origin")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = timeout
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Write(ctx context.Context, body []byte) error {
	var request logtransform.WriteRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || !idPattern.MatchString(request.MessageID) || !datePattern.MatchString(request.IndexDate) || len(request.Document) == 0 {
		return errors.New("invalid log write request")
	}
	// Elasticsearch templates are external cluster state. Re-ensure the fixed
	// contract for every write so replacing or resetting a live cluster cannot
	// leave this process relying on stale in-memory readiness.
	if err := c.ensureTemplate(ctx); err != nil {
		return err
	}
	index := IndexPrefix + request.IndexDate
	path := "/" + index + "/_doc/" + request.MessageID
	response, err := c.do(ctx, http.MethodPut, path, bytes.NewReader(request.Document))
	if err != nil {
		return err
	}
	payload, readErr := readLimited(response.Body)
	response.Body.Close()
	if readErr != nil || (response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated) {
		return errors.New("Elasticsearch rejected log document")
	}
	var result struct {
		Index  string `json:"_index"`
		ID     string `json:"_id"`
		Result string `json:"result"`
	}
	if json.Unmarshal(payload, &result) != nil || result.Index != index || result.ID != request.MessageID || (result.Result != "created" && result.Result != "updated" && result.Result != "noop") {
		return errors.New("Elasticsearch returned an invalid write result")
	}
	// A successful document response is not sufficient: a cluster replacement
	// between template ensure and auto-create could still produce an unqueryable
	// index. Keep the Kafka offset uncommitted until strict mapping and alias are
	// observed on the actual target index.
	return c.verifyIndexContract(ctx, index)
}

func (c *Client) Ready(ctx context.Context) error {
	response, err := c.do(ctx, http.MethodGet, "/_cluster/health", nil)
	if err != nil {
		return err
	}
	_, readErr := readLimited(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Elasticsearch unavailable")
	}
	return c.ensureTemplate(ctx)
}

func (c *Client) ensureTemplate(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	response, err := c.do(ctx, http.MethodPut, "/_index_template/"+TemplateName, strings.NewReader(templateBody))
	if err != nil {
		return err
	}
	payload, readErr := readLimited(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Elasticsearch log template unavailable")
	}
	var result struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if json.Unmarshal(payload, &result) != nil || !result.Acknowledged {
		return errors.New("Elasticsearch log template acknowledgement invalid")
	}
	return nil
}

func (c *Client) verifyIndexContract(ctx context.Context, index string) error {
	mappingResponse, err := c.do(ctx, http.MethodGet, "/"+index+"/_mapping", nil)
	if err != nil {
		return err
	}
	mappingPayload, readErr := readLimited(mappingResponse.Body)
	mappingResponse.Body.Close()
	if readErr != nil || mappingResponse.StatusCode < 200 || mappingResponse.StatusCode >= 300 || !validMapping(mappingPayload, index) {
		return errors.New("Elasticsearch log index mapping incompatible")
	}

	aliasResponse, err := c.do(ctx, http.MethodGet, "/"+index+"/_alias/"+ReadAlias, nil)
	if err != nil {
		return err
	}
	aliasPayload, readErr := readLimited(aliasResponse.Body)
	aliasResponse.Body.Close()
	if readErr != nil || aliasResponse.StatusCode < 200 || aliasResponse.StatusCode >= 300 || !validAlias(aliasPayload, index) {
		return errors.New("Elasticsearch log index alias unavailable")
	}
	return nil
}

func validMapping(payload []byte, index string) bool {
	var response map[string]struct {
		Mappings struct {
			Dynamic    string `json:"dynamic"`
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		} `json:"mappings"`
	}
	if json.Unmarshal(payload, &response) != nil || len(response) != 1 {
		return false
	}
	entry, ok := response[index]
	if !ok || entry.Mappings.Dynamic != "strict" || len(entry.Mappings.Properties) != len(requiredPropertyTypes) {
		return false
	}
	for field, expected := range requiredPropertyTypes {
		property, ok := entry.Mappings.Properties[field]
		if !ok || property.Type != expected {
			return false
		}
	}
	return true
}

func validAlias(payload []byte, index string) bool {
	var response map[string]struct {
		Aliases map[string]json.RawMessage `json:"aliases"`
	}
	if json.Unmarshal(payload, &response) != nil || len(response) != 1 {
		return false
	}
	entry, ok := response[index]
	if !ok {
		return false
	}
	_, ok = entry.Aliases[ReadAlias]
	return ok
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, errors.New("Elasticsearch request creation failed")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, errors.New("Elasticsearch request failed")
	}
	return response, nil
}
func readLimited(body io.Reader) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(body, responseLimit+1))
	if err != nil || len(value) > responseLimit {
		return nil, errors.New("Elasticsearch response exceeded limit")
	}
	return value, nil
}

const templateBody = `{"index_patterns":["gopulse-logs-v1-*"],"template":{"aliases":{"gopulse-logs-v1-read":{}},"mappings":{"dynamic":"strict","properties":{"@timestamp":{"type":"date_nanos"},"log_schema_version":{"type":"integer"},"level":{"type":"keyword"},"service":{"type":"keyword"},"module":{"type":"keyword"},"message":{"type":"keyword"},"request_id":{"type":"keyword"},"event_id":{"type":"keyword"},"event_type":{"type":"keyword"},"user_id":{"type":"long"},"post_id":{"type":"long"},"comment_id":{"type":"long"},"notification_id":{"type":"long"},"outbox_id":{"type":"long"},"method":{"type":"keyword"},"route":{"type":"keyword"},"status":{"type":"long"},"duration_ms":{"type":"long"},"response_bytes":{"type":"long"},"error_code":{"type":"keyword"},"reason":{"type":"keyword"},"operation":{"type":"keyword"},"resource":{"type":"keyword"},"stage":{"type":"keyword"},"result":{"type":"keyword"},"attempt":{"type":"long"},"batch_size":{"type":"long"},"document_count":{"type":"long"},"panic_recovered":{"type":"boolean"},"response_committed":{"type":"boolean"}}}}}`
