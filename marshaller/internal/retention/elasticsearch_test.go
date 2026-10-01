package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestElasticsearchRequiresFixedClusterMappingMarkerAndAlias(t *testing.T) {
	const index = "gopulse-logs-v1-2026.09.20"
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			_, _ = w.Write([]byte(`{"cluster_uuid":"cluster-a"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/_mapping"):
			_, _ = w.Write(mappingResponse(index, Logs, true))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/_alias/gopulse-logs-v1-read"):
			_, _ = w.Write([]byte(`{"gopulse-logs-v1-2026.09.20":{"aliases":{"gopulse-logs-v1-read":{}}}}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/_settings"):
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/"+index:
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store, err := NewElasticsearch(server.URL, time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultConfig().Logs
	ownership, err := store.Inspect(context.Background(), index, policy, "cluster-a")
	if err != nil || !ownership.Valid || !ownership.Marker || !ownership.Mapping || !ownership.Alias {
		t.Fatalf("ownership=%+v err=%v", ownership, err)
	}
	if err := store.BlockWrites(context.Background(), index); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteIndex(context.Background(), index); err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		if strings.Contains(request, "gopulse-post") || strings.Contains(request, "_all") {
			t.Fatalf("retention touched an unrelated resource: %s", request)
		}
	}
}

func TestElasticsearchRejectsUnmarkedSamePrefixIndex(t *testing.T) {
	const index = "gopulse-logs-v1-2026.09.20"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			_, _ = w.Write([]byte(`{"cluster_uuid":"cluster-a"}`))
		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			_, _ = w.Write(mappingResponse(index, Logs, false))
		case strings.Contains(r.URL.Path, "/_alias/"):
			_, _ = w.Write([]byte(`{"gopulse-logs-v1-2026.09.20":{"aliases":{"gopulse-logs-v1-read":{}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	store, err := NewElasticsearch(server.URL, time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	ownership, err := store.Inspect(context.Background(), index, DefaultConfig().Logs, "cluster-a")
	if err != nil || ownership.Valid || !ownership.Mapping || ownership.Marker {
		t.Fatalf("unmarked ownership=%+v err=%v", ownership, err)
	}
}

func mappingResponse(index string, stream Stream, marker bool) []byte {
	properties := map[string]any{}
	if stream == Logs {
		for _, name := range []string{"version", "revision", "event", "runtime_contract_version", "runtime_mode", "listen", "@timestamp", "log_schema_version", "level", "service", "instance_id", "module", "message", "request_id", "trace_id", "span_id", "attempt_id", "event_id", "event_type", "user_id", "post_id", "content_revision", "comment_id", "notification_id", "outbox_id", "method", "route", "status", "duration_ms", "response_bytes", "error_code", "reason", "operation", "resource", "stage", "result", "attempt", "batch_size", "document_count", "panic_recovered", "response_committed"} {
			properties[name] = map[string]string{"type": "keyword"}
		}
	}
	meta := map[string]any{}
	if marker {
		meta = map[string]any{"gopulse_product": "gopulse-observability", "gopulse_stream": string(stream), "gopulse_schema": "v1"}
	}
	value := map[string]any{index: map[string]any{"mappings": map[string]any{"dynamic": "strict", "_meta": meta, "properties": properties}}}
	body, _ := json.Marshal(value)
	return body
}

type retentionHTTP struct {
	base   string
	client *http.Client
}

func (h retentionHTTP) request(method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = strings.NewReader(string(payload))
	}
	request, err := http.NewRequest(method, strings.TrimRight(h.base, "/")+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := h.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, data, readErr
}

func realElasticsearchURL() string { return os.Getenv("PHASE20_RETENTION_ES_URL") }

func TestRealElasticsearchRetentionIntegration(t *testing.T) {
	base := realElasticsearchURL()
	if base == "" {
		t.Skip("PHASE20_RETENTION_ES_URL is not set")
	}
	reportPath := os.Getenv("PHASE20_RETENTION_REPORT")
	httpClient := retentionHTTP{base: base, client: &http.Client{Timeout: 5 * time.Second}}
	store, err := NewElasticsearch(base, 3*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.ClusterIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	day := UTCDate(now)
	oldDate := day.AddDate(0, 0, -10)
	currentDate := day
	futureDate := day.AddDate(0, 0, 1)
	logs := DefaultConfig().Logs
	events := DefaultConfig().Events
	fixture := map[string]any{
		"cluster_uuid": identity,
		"started_at":   now.Format(time.RFC3339Nano),
		"cutoff":       logs.Cutoff(now).Format(DateLayout),
		"old":          map[string]any{},
		"current":      map[string]any{},
		"sequence":     []string{},
	}
	created := []string{}
	defer func() {
		for _, index := range created {
			_, _, _ = httpClient.request(http.MethodDelete, "/"+index, nil)
		}
		if reportPath != "" {
			payload, marshalErr := json.MarshalIndent(fixture, "", "  ")
			if marshalErr == nil {
				_ = os.MkdirAll(filepath.Dir(reportPath), 0o700)
				_ = os.WriteFile(reportPath, append(payload, '\n'), 0o600)
			}
		}
	}()

	create := func(index string, policy Policy, marked, withAlias bool) {
		t.Helper()
		aliases := map[string]any{}
		if withAlias {
			aliases[policy.Alias] = map[string]any{}
		}
		body := map[string]any{
			"aliases": aliases,
			"mappings": map[string]any{
				"dynamic": "strict",
				"_meta": func() map[string]string {
					if !marked {
						return map[string]string{}
					}
					return map[string]string{"gopulse_product": "gopulse-observability", "gopulse_stream": string(policy.Stream), "gopulse_schema": "v1"}
				}(),
				"properties": integrationProperties(policy.Stream),
			},
		}
		status, response, createErr := httpClient.request(http.MethodPut, "/"+index, body)
		if createErr != nil || (status != http.StatusOK && status != http.StatusCreated) {
			t.Fatalf("create %s status=%d err=%v body=%s", index, status, createErr, response)
		}
		created = append(created, index)
	}
	putDocument := func(index, id string) {
		t.Helper()
		status, response, putErr := httpClient.request(http.MethodPut, "/"+index+"/_doc/"+id+"?refresh=wait_for", map[string]any{"@timestamp": now.Format(time.RFC3339Nano), "message": id})
		if putErr != nil || (status != http.StatusOK && status != http.StatusCreated) {
			t.Fatalf("put %s status=%d err=%v body=%s", index, status, putErr, response)
		}
	}
	exists := func(index string) bool {
		status, _, requestErr := httpClient.request(http.MethodHead, "/"+index, nil)
		return requestErr == nil && status == http.StatusOK
	}
	searchCount := func(alias string) int {
		status, response, searchErr := httpClient.request(http.MethodGet, "/"+alias+"/_search?size=0", nil)
		if searchErr != nil || status != http.StatusOK {
			t.Fatalf("search %s status=%d err=%v body=%s", alias, status, searchErr, response)
		}
		var value struct {
			Hits struct {
				Total struct {
					Value int `json:"value"`
				} `json:"total"`
			} `json:"hits"`
		}
		if json.Unmarshal(response, &value) != nil {
			t.Fatalf("invalid search response: %s", response)
		}
		return value.Hits.Total.Value
	}

	oldLog := logs.Prefix + oldDate.Format(DateLayout)
	currentLog := logs.Prefix + currentDate.Format(DateLayout)
	futureLog := logs.Prefix + futureDate.Format(DateLayout)
	oldEvent := events.Prefix + oldDate.Format(DateLayout)
	currentEvent := events.Prefix + currentDate.Format(DateLayout)
	oldUnmarked := logs.Prefix + oldDate.AddDate(0, 0, -1).Format(DateLayout)
	invalid := logs.Prefix + "2026.02.30"
	business := "gopulse-business-fixture-" + strings.ToLower(strings.ReplaceAll(identity, "-", "")[:8])
	for _, fixtureIndex := range []struct {
		name   string
		policy Policy
		marked bool
		alias  bool
	}{
		{oldLog, logs, true, true}, {currentLog, logs, true, true}, {futureLog, logs, true, true},
		{oldEvent, events, true, true}, {currentEvent, events, true, true},
		{oldUnmarked, logs, false, true}, {invalid, logs, true, true},
	} {
		create(fixtureIndex.name, fixtureIndex.policy, fixtureIndex.marked, fixtureIndex.alias)
		putDocument(fixtureIndex.name, strings.Repeat("a", 31)+fmt.Sprintf("%d", len(created)%10))
	}
	status, response, businessErr := httpClient.request(http.MethodPut, "/"+business, map[string]any{"mappings": map[string]any{"properties": map[string]any{"value": map[string]string{"type": "keyword"}}}})
	if businessErr != nil || (status != http.StatusOK && status != http.StatusCreated) {
		t.Fatalf("create business fixture status=%d err=%v body=%s", status, businessErr, response)
	}
	created = append(created, business)
	status, response, businessDocErr := httpClient.request(http.MethodPut, "/"+business+"/_doc/business-doc-1?refresh=wait_for", map[string]any{"value": "business"})
	if businessDocErr != nil || (status != http.StatusOK && status != http.StatusCreated) {
		t.Fatalf("put business fixture status=%d err=%v body=%s", status, businessDocErr, response)
	}

	fixture["old"].(map[string]any)["logs_before"] = map[string]any{"exists": exists(oldLog), "documents": searchCount(logs.Alias)}
	fixture["current"].(map[string]any)["logs_before"] = map[string]any{"exists": exists(currentLog)}
	fixture["sequence"] = append(fixture["sequence"].([]string), "R01 boundary and ownership fixtures created", "R02 pre-delete inventory/documents recorded")
	runner, err := NewRunner(store, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	runner.now = func() time.Time { return now }
	report, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !containsResult(report, oldLog, "deleted") || !containsResult(report, oldEvent, "deleted") {
		t.Fatalf("owned expired indices were not deleted: %+v", report)
	}
	for _, index := range []string{oldLog, oldEvent} {
		if exists(index) {
			t.Fatalf("expired index remains: %s", index)
		}
	}
	for _, index := range []string{currentLog, currentEvent, futureLog, oldUnmarked, invalid, business} {
		if !exists(index) {
			t.Fatalf("non-expired or unrelated index was removed: %s", index)
		}
	}
	logCount, eventCount := searchCount(logs.Alias), searchCount(events.Alias)
	if logCount == 0 || eventCount == 0 {
		t.Fatalf("current logs/events are not queryable through fixed aliases: logs=%d events=%d current_log=%t current_event=%t", logCount, eventCount, exists(currentLog), exists(currentEvent))
	}
	fixture["old"].(map[string]any)["logs_after"] = map[string]any{"exists": exists(oldLog)}
	fixture["old"].(map[string]any)["events_after"] = map[string]any{"exists": exists(oldEvent)}
	fixture["current"].(map[string]any)["logs_after"] = map[string]any{"exists": exists(currentLog), "alias_documents": searchCount(logs.Alias)}
	fixture["current"].(map[string]any)["events_after"] = map[string]any{"exists": exists(currentEvent), "alias_documents": searchCount(events.Alias)}
	fixture["sequence"] = append(fixture["sequence"].([]string), "R02 deletion completed and current aliases queried")

	transientIndex := logs.Prefix + oldDate.AddDate(0, 0, -2).Format(DateLayout)
	create(transientIndex, logs, true, true)
	fault := &integrationFaultStore{Store: store, transient: 1}
	catchup, err := NewRunner(fault, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	catchup.now = func() time.Time { return now }
	catchupReport, err := catchup.RunOnce(context.Background())
	if err != nil || !containsResult(catchupReport, transientIndex, "deleted") {
		t.Fatalf("transient catch-up failed: err=%v report=%+v", err, catchupReport)
	}
	permissionIndex := logs.Prefix + oldDate.AddDate(0, 0, -3).Format(DateLayout)
	create(permissionIndex, logs, true, true)
	permission := &integrationFaultStore{Store: store, permission: true}
	permissionRunner, err := NewRunner(permission, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	permissionRunner.now = func() time.Time { return now }
	permissionReport, err := permissionRunner.RunOnce(context.Background())
	if err != nil || !containsResult(permissionReport, permissionIndex, "permission_denied") || !exists(permissionIndex) {
		t.Fatalf("permission failure was hidden: err=%v report=%+v", err, permissionReport)
	}
	fixture["sequence"] = append(fixture["sequence"].([]string), "R05 transient delete retried and permission delete remained blocked")

	idempotentIndex := logs.Prefix + oldDate.AddDate(0, 0, -4).Format(DateLayout)
	create(idempotentIndex, logs, true, true)
	runners := []*Runner{}
	for range 2 {
		copyRunner, runnerErr := NewRunner(store, DefaultConfig())
		if runnerErr != nil {
			t.Fatal(runnerErr)
		}
		copyRunner.now = func() time.Time { return now }
		runners = append(runners, copyRunner)
	}
	var group sync.WaitGroup
	results := make([]Report, len(runners))
	group.Add(len(runners))
	for i, item := range runners {
		go func(index int, current *Runner) {
			defer group.Done()
			results[index], _ = current.RunOnce(context.Background())
		}(i, item)
	}
	group.Wait()
	if exists(idempotentIndex) || !containsAnyResult(results, idempotentIndex, "deleted", "not_found") {
		t.Fatalf("concurrent idempotent cleanup failed: results=%+v", results)
	}
	fixture["sequence"] = append(fixture["sequence"].([]string), "R06 two runners completed idempotent cleanup")

	fixture["cases"] = []string{"R01", "R02", "R03", "R04", "R05", "R06", "R07"}
}

func integrationProperties(stream Stream) map[string]any {
	properties := map[string]any{}
	if stream == Events {
		metadata := map[string]any{}
		for _, name := range []string{"plugin_id", "plugin_version", "previous_plugin_version", "operation", "from_state", "to_state", "error_code", "scrape_status"} {
			metadata[name] = map[string]string{"type": "keyword"}
		}
		return map[string]any{
			"@timestamp": map[string]string{"type": "date_nanos"}, "event_schema_version": map[string]string{"type": "integer"},
			"event_name": map[string]string{"type": "keyword"}, "source": map[string]string{"type": "keyword"}, "severity": map[string]string{"type": "keyword"}, "message": map[string]string{"type": "keyword"},
			"metadata": map[string]any{"type": "object", "dynamic": "strict", "properties": metadata},
		}
	}
	for _, name := range []string{"version", "revision", "event", "runtime_contract_version", "runtime_mode", "listen", "@timestamp", "log_schema_version", "level", "service", "instance_id", "module", "message", "request_id", "trace_id", "span_id", "attempt_id", "event_id", "event_type", "user_id", "post_id", "content_revision", "comment_id", "notification_id", "outbox_id", "method", "route", "status", "duration_ms", "response_bytes", "error_code", "reason", "operation", "resource", "stage", "result", "attempt", "batch_size", "document_count", "panic_recovered", "response_committed"} {
		properties[name] = map[string]string{"type": "keyword"}
	}
	return properties
}

func containsResult(report Report, index, result string) bool {
	for _, item := range report.Items {
		if item.Index == index && item.Result == result {
			return true
		}
	}
	return false
}

func containsAnyResult(reports []Report, index string, results ...string) bool {
	for _, report := range reports {
		for _, result := range results {
			if containsResult(report, index, result) {
				return true
			}
		}
	}
	return false
}

type integrationFaultStore struct {
	Store
	mu         sync.Mutex
	transient  int
	permission bool
}

func (s *integrationFaultStore) DeleteIndex(ctx context.Context, index string) error {
	s.mu.Lock()
	if s.transient > 0 {
		s.transient--
		s.mu.Unlock()
		return &HTTPError{Status: http.StatusServiceUnavailable, Op: "injected delete"}
	}
	permission := s.permission
	s.mu.Unlock()
	if permission {
		return &HTTPError{Status: http.StatusForbidden, Op: "injected delete"}
	}
	return s.Store.DeleteIndex(ctx, index)
}

type integrationBarrierStore struct {
	Store
	once      sync.Once
	inspected chan struct{}
	release   chan struct{}
	deleted   chan struct{}
}

func (s *integrationBarrierStore) Inspect(ctx context.Context, index string, policy Policy, cluster string) (Ownership, error) {
	ownership, err := s.Store.Inspect(ctx, index, policy, cluster)
	s.once.Do(func() {
		close(s.inspected)
		<-s.release
	})
	return ownership, err
}

func (s *integrationBarrierStore) DeleteIndex(ctx context.Context, index string) error {
	err := s.Store.DeleteIndex(ctx, index)
	if err == nil || IsNotFound(err) {
		select {
		case s.deleted <- struct{}{}:
		default:
		}
	}
	return err
}
