package eventquery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
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
	for _, values := range []url.Values{
		{"event_name": {"exporter_plugin_started"}, "operation": {"stop"}},
		{"source": {"backend"}}, {"error_code": {"unknown_failed"}}, {"event_name": {"exporter_plugin_started"}, "error_code": {"start_failed"}}, {"cursor": {"abc"}, "limit": {"1"}}, {"from": {"2026-09-04T00:00:00Z"}, "to": {"2026-09-05T08:00:00Z"}},
	} {
		if _, err := ParseOptions(values, now); err == nil {
			t.Fatalf("invalid values accepted: %v", values)
		}
	}
}

type fakeRepository struct {
	openErr  error
	search   SearchResult
	err      error
	opens    int
	searches int
}

func (r *fakeRepository) OpenPointInTime(context.Context) (string, error) {
	r.opens++
	return "pit", r.openErr
}
func (r *fakeRepository) Search(context.Context, string, Filters, int, *Sort) (SearchResult, error) {
	r.searches++
	return r.search, r.err
}
func (r *fakeRepository) ClosePointInTime(context.Context, string) error { return nil }

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
