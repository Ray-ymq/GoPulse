package count

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type performer func(context.Context, *http.Request) (*http.Response, error)

func (f performer) Perform(c context.Context, r *http.Request) (*http.Response, error) {
	return f(c, r)
}
func TestBoundedCount(t *testing.T) {
	now := time.Date(2026, 9, 12, 0, 0, 0, 123, time.UTC)
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"zero", `{"count":0,"_shards":{"total":1,"successful":1,"failed":0}}`, 200, true},
		{"count", `{"count":7,"_shards":{"total":1,"successful":1,"failed":0}}`, 200, true},
		{"missing", `{}`, 404, false}, {"authentication", `{}`, 401, false},
		{"fraction", `{"count":0.5,"_shards":{"total":1,"successful":1,"failed":0}}`, 200, false},
		{"partial", `{"count":0,"_shards":{"total":1,"successful":0,"failed":1}}`, 200, false},
		{"negative", `{"count":-1,"_shards":{"total":1,"successful":1,"failed":0}}`, 200, false},
		{"overflow", `{"count":9007199254740992,"_shards":{"total":1,"successful":1,"failed":0}}`, 200, false},
		{"trailing", `{"count":0} {}`, 200, false}, {"oversize", strings.Repeat(" ", 65537), 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := performer(func(ctx context.Context, r *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 2*time.Second {
					t.Fatal("unbounded timeout")
				}
				body := `{"fields":{"@timestamp":{"date_nanos":{"type":"date_nanos","searchable":true}},"metadata.operation":{"keyword":{"type":"keyword","searchable":true}}}}`
				status := 200
				if calls == 2 {
					if r.Method != "POST" || r.URL.Path != "/fixed/_count" {
						t.Fatal(r.URL)
					}
					var q map[string]any
					if json.NewDecoder(r.Body).Decode(&q) != nil {
						t.Fatal("body")
					}
					filters := q["query"].(map[string]any)["bool"].(map[string]any)["filter"].([]any)
					boundary := filters[0].(map[string]any)["range"].(map[string]any)["@timestamp"].(map[string]any)
					if boundary["lte"] != now.Format(time.RFC3339Nano) || boundary["gte"] != now.Add(-time.Minute).Format(time.RFC3339Nano) || len(q) != 1 {
						t.Fatal(q)
					}
					body = tc.body
					status = tc.status
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			_, e := Query(context.Background(), client, "fixed", map[string]string{"metadata.operation": "start"}, now.Add(-time.Minute), now)
			if (e == nil) != tc.ok {
				t.Fatalf("%v", e)
			}
		})
	}
}
func TestMappingMismatchIsUnknown(t *testing.T) {
	client := performer(func(context.Context, *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"fields":{}}`))}, nil
	})
	if _, e := Query(context.Background(), client, "fixed", map[string]string{"service": "backend"}, time.Now().Add(-time.Minute), time.Now()); e == nil {
		t.Fatal("missing mapping became zero")
	}
}
