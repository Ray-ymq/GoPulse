package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	eventtransform "github.com/Ray-ymq/GoPulse/marshaller/internal/events"
)

const (
	EventTemplateName = "gopulse-events-v1-template"
	EventIndexPrefix  = "gopulse-events-v1-"
	EventReadAlias    = "gopulse-events-v1-read"
)

type EventsClient struct {
	transport *Client
	mu        sync.Mutex
}

func NewEvents(baseURL string, timeout time.Duration) (*EventsClient, error) {
	transport, err := New(baseURL, timeout)
	if err != nil {
		return nil, err
	}
	return &EventsClient{transport: transport}, nil
}

func (c *EventsClient) Write(ctx context.Context, body []byte) error {
	var request eventtransform.WriteRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || !idPattern.MatchString(request.MessageID) || !datePattern.MatchString(request.IndexDate) || len(request.Document) == 0 {
		return errors.New("invalid event write request")
	}
	if err := c.ensureTemplate(ctx); err != nil {
		return err
	}
	index := EventIndexPrefix + request.IndexDate
	if err := c.ensureIndexMapping(ctx, index); err != nil {
		return err
	}
	response, err := c.transport.do(ctx, http.MethodPut, "/"+index+"/_doc/"+request.MessageID, bytes.NewReader(request.Document))
	if err != nil {
		return err
	}
	payload, readErr := readLimited(response.Body)
	response.Body.Close()
	if readErr != nil || (response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated) {
		return errors.New("Elasticsearch rejected event document")
	}
	var result struct {
		Index  string `json:"_index"`
		ID     string `json:"_id"`
		Result string `json:"result"`
	}
	if json.Unmarshal(payload, &result) != nil || result.Index != index || result.ID != request.MessageID || (result.Result != "created" && result.Result != "updated" && result.Result != "noop") {
		return errors.New("Elasticsearch returned an invalid event write result")
	}
	return c.verifyIndexContract(ctx, index)
}

func (c *EventsClient) Ready(ctx context.Context) error {
	response, err := c.transport.do(ctx, http.MethodGet, "/_cluster/health", nil)
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

func (c *EventsClient) ensureTemplate(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	response, err := c.transport.do(ctx, http.MethodPut, "/_index_template/"+EventTemplateName, strings.NewReader(eventTemplateBody))
	if err != nil {
		return err
	}
	payload, readErr := readLimited(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Elasticsearch event template unavailable")
	}
	var result struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if json.Unmarshal(payload, &result) != nil || !result.Acknowledged {
		return errors.New("Elasticsearch event template acknowledgement invalid")
	}
	return nil
}

func (c *EventsClient) ensureIndexMapping(ctx context.Context, index string) error {
	response, err := c.transport.do(ctx, http.MethodPut, "/"+index+"/_mapping", strings.NewReader(eventMappingExtensionBody))
	if err != nil {
		return err
	}
	payload, readErr := readLimited(response.Body)
	response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Elasticsearch event mapping update unavailable")
	}
	var result struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if json.Unmarshal(payload, &result) != nil || !result.Acknowledged {
		return errors.New("Elasticsearch event mapping acknowledgement invalid")
	}
	return nil
}

func (c *EventsClient) verifyIndexContract(ctx context.Context, index string) error {
	response, err := c.transport.do(ctx, http.MethodGet, "/"+index+"/_mapping", nil)
	if err != nil {
		return err
	}
	payload, readErr := readLimited(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 || !validEventMapping(payload, index) {
		return errors.New("Elasticsearch event index mapping incompatible")
	}
	response, err = c.transport.do(ctx, http.MethodGet, "/"+index+"/_alias/"+EventReadAlias, nil)
	if err != nil {
		return err
	}
	payload, readErr = readLimited(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 || !validEventAlias(payload, index) {
		return errors.New("Elasticsearch event index alias unavailable")
	}
	return nil
}

func validEventMapping(payload []byte, index string) bool {
	type property struct {
		Type       string              `json:"type"`
		Dynamic    string              `json:"dynamic"`
		Properties map[string]property `json:"properties"`
	}
	var response map[string]struct {
		Mappings struct {
			Dynamic    string              `json:"dynamic"`
			Properties map[string]property `json:"properties"`
		} `json:"mappings"`
	}
	if json.Unmarshal(payload, &response) != nil || len(response) != 1 {
		return false
	}
	entry, ok := response[index]
	if !ok || entry.Mappings.Dynamic != "strict" || len(entry.Mappings.Properties) != 7 {
		return false
	}
	expected := map[string]string{"@timestamp": "date_nanos", "event_schema_version": "integer", "event_name": "keyword", "source": "keyword", "severity": "keyword", "message": "keyword", "metadata": "object"}
	for field, fieldType := range expected {
		property := entry.Mappings.Properties[field]
		if field == "metadata" {
			if property.Type != "" && property.Type != fieldType {
				return false
			}
			continue
		}
		if property.Type != fieldType {
			return false
		}
	}
	metadata := entry.Mappings.Properties["metadata"]
	if metadata.Dynamic != "strict" || len(metadata.Properties) != 8 {
		return false
	}
	for _, field := range []string{"plugin_id", "plugin_version", "previous_plugin_version", "operation", "from_state", "to_state", "error_code", "scrape_status"} {
		if metadata.Properties[field].Type != "keyword" {
			return false
		}
	}
	return true
}

func validEventAlias(payload []byte, index string) bool {
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
	_, ok = entry.Aliases[EventReadAlias]
	return ok
}

const eventMappingExtensionBody = `{"properties":{"metadata":{"properties":{"error_code":{"type":"keyword"},"scrape_status":{"type":"keyword"}}}}}`

const eventTemplateBody = `{"index_patterns":["gopulse-events-v1-*"],"template":{"aliases":{"gopulse-events-v1-read":{}},"mappings":{"dynamic":"strict","properties":{"@timestamp":{"type":"date_nanos"},"event_schema_version":{"type":"integer"},"event_name":{"type":"keyword"},"source":{"type":"keyword"},"severity":{"type":"keyword"},"message":{"type":"keyword"},"metadata":{"type":"object","dynamic":"strict","properties":{"plugin_id":{"type":"keyword"},"plugin_version":{"type":"keyword"},"previous_plugin_version":{"type":"keyword"},"operation":{"type":"keyword"},"from_state":{"type":"keyword"},"to_state":{"type":"keyword"},"error_code":{"type":"keyword"},"scrape_status":{"type":"keyword"}}}}}}}`
