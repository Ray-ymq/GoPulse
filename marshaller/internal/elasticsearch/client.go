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

type Client struct {
	baseURL string
	client  *http.Client
	mu      sync.Mutex
	ready   bool
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
	if err := c.ensureTemplate(ctx); err != nil {
		return err
	}
	index := IndexPrefix + request.IndexDate
	path := "/" + index + "/_doc/" + request.MessageID
	response, err := c.do(ctx, http.MethodPut, path, bytes.NewReader(request.Document))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	payload, err := readLimited(response.Body)
	if err != nil || (response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated) {
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
	return nil
}

func (c *Client) Ready(ctx context.Context) error {
	response, err := c.do(ctx, http.MethodGet, "/_cluster/health", nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, readErr := readLimited(response.Body)
	if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Elasticsearch unavailable")
	}
	return nil
}

func (c *Client) ensureTemplate(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ready {
		return nil
	}
	response, err := c.do(ctx, http.MethodPut, "/_index_template/"+TemplateName, strings.NewReader(templateBody))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, readErr := readLimited(response.Body)
	if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Elasticsearch log template unavailable")
	}
	c.ready = true
	return nil
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
