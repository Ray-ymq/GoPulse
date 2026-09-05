package eventquery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestParseOptionsUsesBoundedKnownVocabulary(t *testing.T) {
	now := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
	options, err := ParseOptions(url.Values{"event_name": {"exporter_plugin_started"}, "plugin_id": {"redis-exporter"}, "operation": {"start"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if options.Limit != 50 || options.Filters.From != "2026-09-05T07:45:00Z" || options.Filters.To != "2026-09-05T08:00:00Z" {
		t.Fatalf("unexpected options: %+v", options)
	}
	longCursor := strings.Repeat("a", 300)
	cursorOptions, err := ParseOptions(url.Values{"cursor": {longCursor}}, now)
	if err != nil || cursorOptions.Cursor != longCursor {
		t.Fatalf("valid-length cursor rejected: options=%+v err=%v", cursorOptions, err)
	}
	for _, values := range []url.Values{
		{"event_name": {"exporter_plugin_started"}, "operation": {"stop"}},
		{"source": {"backend"}}, {"error_code": {"unknown_failed"}}, {"event_name": {"exporter_plugin_started"}, "error_code": {"start_failed"}},
		{"event_name": {"exporter_plugin_failed"}, "error_code": {"publish_failed"}}, {"event_name": {"metrics_collection_failed"}, "error_code": {"start_failed"}},
		{"cursor": {"abc"}, "limit": {"1"}}, {"from": {"2026-09-04T00:00:00Z"}, "to": {"2026-09-05T08:00:00Z"}},
	} {
		if _, err := ParseOptions(values, now); err == nil {
			t.Fatalf("invalid values accepted: %v", values)
		}
	}
}

type fakeRepository struct {
	openErr    error
	search     SearchResult
	err        error
	opens      int
	searches   int
	searchFunc func(string, Filters, int, *Sort) (SearchResult, error)
	closedPITs []string
}

func (r *fakeRepository) OpenPointInTime(context.Context) (string, error) {
	r.opens++
	return "pit", r.openErr
}
func (r *fakeRepository) Search(_ context.Context, pit string, filters Filters, limit int, after *Sort) (SearchResult, error) {
	r.searches++
	if r.searchFunc != nil {
		return r.searchFunc(pit, filters, limit, after)
	}
	return r.search, r.err
}
func (r *fakeRepository) ClosePointInTime(_ context.Context, pit string) error {
	r.closedPITs = append(r.closedPITs, pit)
	return nil
}

func TestServiceHandlesMissingAliasAndStorageFailure(t *testing.T) {
	repository := &fakeRepository{openErr: ErrAliasMissing}
	service := NewService(repository, strings.Repeat("s", 32))
	service.now = func() time.Time { return time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC) }
	page, err := service.Query(context.Background(), Options{Limit: 50, Filters: Filters{From: "2026-09-05T07:45:00Z", To: "2026-09-05T08:00:00Z"}})
	if err != nil || page.Entries == nil || repository.searches != 0 {
		t.Fatalf("page=%+v err=%v searches=%d", page, err, repository.searches)
	}
	repository.openErr = errors.New("down")
	if _, err := service.Query(context.Background(), Options{Limit: 50}); err == nil {
		t.Fatal("storage failure was not returned")
	}
}

type performerFunc func(context.Context, *http.Request) (*http.Response, error)

func (f performerFunc) Perform(ctx context.Context, request *http.Request) (*http.Response, error) {
	return f(ctx, request)
}

func TestRepositoryUsesFixedAliasAndSafeSourceContract(t *testing.T) {
	var path string
	repository := NewElasticsearchRepository(performerFunc(func(_ context.Context, request *http.Request) (*http.Response, error) {
		path = request.URL.String()
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"pit"}`))}, nil
	}))
	pit, err := repository.OpenPointInTime(context.Background())
	if err != nil || pit != "pit" || !strings.HasPrefix(path, "/gopulse-events-v1-read/_pit?") {
		t.Fatalf("pit=%q path=%q err=%v", pit, path, err)
	}
	valid := []byte(`{"@timestamp":"2026-09-05T08:00:00Z","event_schema_version":1,"event_name":"exporter_plugin_started","source":"monitor","severity":"info","message":"exporter plugin started","metadata":{"plugin_id":"redis-exporter","plugin_version":"1.7.1","operation":"start","from_state":"stopped","to_state":"running"}}`)
	entry, err := decodeEntry(valid)
	if err != nil || entry.Metadata.PluginVersion != "1.7.1" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	if _, err := decodeEntry([]byte(strings.Replace(string(valid), `"plugin_version":"1.7.1"`, `"plugin_version":"secret-token"`, 1))); err == nil {
		t.Fatal("unsafe document was accepted")
	}
}

func TestServicePaginatesWithSignedCursorAndClosesTerminalPIT(t *testing.T) {
	now := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
	filters := Filters{From: "2026-09-05T07:45:00Z", To: "2026-09-05T08:00:00Z", Source: "monitor", PluginID: "redis-exporter"}
	firstSort := Sort{Timestamp: "2026-09-05T07:59:00Z", ShardDoc: 12}
	calls := 0
	repository := &fakeRepository{}
	repository.searchFunc = func(pit string, got Filters, limit int, after *Sort) (SearchResult, error) {
		calls++
		if got != filters || limit != 1 {
			t.Fatalf("filters=%+v limit=%d", got, limit)
		}
		switch calls {
		case 1:
			if pit != "pit" || after != nil {
				t.Fatalf("first search pit=%q after=%+v", pit, after)
			}
			return SearchResult{PIT: "pit-2", Hits: []Hit{{Entry: Entry{EventName: "exporter_plugin_started"}, Sort: firstSort}, {Entry: Entry{EventName: "exporter_plugin_stopped"}, Sort: Sort{Timestamp: "2026-09-05T07:58:00Z", ShardDoc: 11}}}}, nil
		case 2:
			if pit != "pit-2" || after == nil || *after != firstSort {
				t.Fatalf("second search pit=%q after=%+v", pit, after)
			}
			return SearchResult{PIT: "pit-3", Hits: []Hit{{Entry: Entry{EventName: "exporter_plugin_stopped"}, Sort: Sort{Timestamp: "2026-09-05T07:58:00Z", ShardDoc: 11}}}}, nil
		default:
			t.Fatalf("unexpected search %d", calls)
			return SearchResult{}, nil
		}
	}
	service := NewService(repository, strings.Repeat("s", 32))
	service.now = func() time.Time { return now }
	first, err := service.Query(context.Background(), Options{Filters: filters, Limit: 1})
	if err != nil || len(first.Entries) != 1 || first.NextCursor == nil || repository.opens != 1 {
		t.Fatalf("first=%+v err=%v opens=%d", first, err, repository.opens)
	}
	second, err := service.Query(context.Background(), Options{Cursor: *first.NextCursor})
	if err != nil || len(second.Entries) != 1 || second.NextCursor != nil {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if len(repository.closedPITs) != 1 || repository.closedPITs[0] != "pit-3" {
		t.Fatalf("closed PITs=%v", repository.closedPITs)
	}
}

func TestHandlerReturnsEmptyPageAndEventsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		repository *fakeRepository
		status     int
		body       string
	}{
		{name: "empty alias", repository: &fakeRepository{openErr: ErrAliasMissing}, status: http.StatusOK, body: `"data":[],"meta":{"next_cursor":null}`},
		{name: "unavailable", repository: &fakeRepository{openErr: errors.New("down")}, status: http.StatusServiceUnavailable, body: `"code":"events_unavailable"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(test.repository, strings.Repeat("s", 32))
			handler := NewHandler(service)
			handler.now = func() time.Time { return now }
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/observability/events", nil)
			handler.List(context)
			if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), test.body) {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
