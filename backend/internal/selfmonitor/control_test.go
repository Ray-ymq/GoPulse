package selfmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeController(active string) (*Controller, *[][]string) {
	calls := [][]string{}
	c := NewController()
	c.run = func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{}, args...))
		return []byte("ActiveState=" + active + "\nSubState=exited\nResult=success\n"), nil
	}
	return c, &calls
}
func TestFixedActionsAndIdempotence(t *testing.T) {
	for _, test := range []struct {
		active, path, want string
		commands           int
	}{
		{"inactive", "/start", "starting", 2}, {"active", "/start", "running", 1}, {"activating", "/start", "starting", 1},
		{"active", "/stop", "stopping", 2}, {"inactive", "/stop", "stopped", 1}, {"deactivating", "/start", "stopping", 1},
		{"failed", "/start", "starting", 2}, {"failed", "/stop", "stopping", 2},
	} {
		t.Run(test.active+test.path, func(t *testing.T) {
			c, calls := fakeController(test.active)
			r := httptest.NewRecorder()
			c.ServeHTTP(r, httptest.NewRequest("POST", test.path, nil))
			var got Status
			if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if r.Code != 200 || got.State != test.want || len(*calls) != test.commands {
				t.Fatalf("%d %#v calls=%v", r.Code, got, *calls)
			}
			if test.commands == 2 && !reflect.DeepEqual((*calls)[1], []string{"--no-block", strings.TrimPrefix(test.path, "/"), unit}) {
				t.Fatal(*calls)
			}
		})
	}
}
func TestRejectUntrustedInputsBeforeCommands(t *testing.T) {
	for _, test := range []struct{ method, path, body string }{
		{"POST", "/start?service=backend", ""}, {"POST", "/start?", ""}, {"POST", "/start", "{}"}, {"GET", "/start", ""}, {"POST", "/restart", ""}, {"POST", "/start/../../stop", ""},
	} {
		c, calls := fakeController("inactive")
		r := httptest.NewRecorder()
		c.ServeHTTP(r, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
		if r.Code < 400 || len(*calls) != 0 {
			t.Fatalf("accepted %#v, %d %v", test, r.Code, *calls)
		}
	}
}
func TestUnavailableNeverMeansStopped(t *testing.T) {
	c := NewController()
	c.run = func(context.Context, ...string) ([]byte, error) { return nil, errors.New("secret command stderr") }
	r := httptest.NewRecorder()
	c.ServeHTTP(r, httptest.NewRequest("GET", "/status", nil))
	if r.Code != 503 || strings.Contains(r.Body.String(), "secret") || !strings.Contains(r.Body.String(), `"state":"unavailable"`) {
		t.Fatal(r.Code, r.Body.String())
	}
}
func TestActiveUnitWithMissingScrapesIsDegraded(t *testing.T) {
	c, _ := fakeController("active")
	c.client = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body := `{"status":"success","data":{"activeTargets":[]}}`
		if strings.Contains(r.URL.Path, "query") {
			body = `{"status":"success","data":{"result":[]}}`
		}
		if strings.Contains(r.URL.Path, "health") {
			body = `{"database":"ok"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	r := httptest.NewRecorder()
	c.ServeHTTP(r, httptest.NewRequest("GET", "/status", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"state":"degraded"`) {
		t.Fatal(r.Code, r.Body.String())
	}
}
func TestConcurrentStartsDoNotCreateRepeatedJobs(t *testing.T) {
	c := NewController()
	active := "inactive"
	starts := 0
	c.run = func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "--no-block" {
			starts++
			active = "activating"
		}
		return []byte("ActiveState=" + active), nil
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { c.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/start", nil)) })
	}
	wg.Wait()
	if starts != 1 {
		t.Fatalf("start requests: %d", starts)
	}
}
func TestClientRejectsOtherActions(t *testing.T) {
	if _, err := NewClient().Do(context.Background(), "restart"); err == nil {
		t.Fatal("accepted arbitrary action")
	}
}

func TestCompleteTargetsAndSourcesAreRunning(t *testing.T) {
	c, _ := fakeController("active")
	jobs := map[string]int{"backend": 3, "business-worker": 2, "search-indexer": 2, "monitor": 1, "marshaller": 2, "node": 1, "mysql": 1, "redis": 1, "rabbitmq": 1, "kafka": 1, "elasticsearch": 1, "observability-elasticsearch": 1, "victoriametrics": 1, "prometheus": 1}
	targets := []any{}
	for job, count := range jobs {
		for i := range count {
			targets = append(targets, map[string]any{"labels": map[string]string{"job": job, "instance": fmt.Sprintf("%s-%d", job, i)}, "health": "up"})
		}
	}
	c.client = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		var body any = map[string]any{"status": "success", "data": map[string]any{"activeTargets": targets}}
		if strings.Contains(r.URL.Path, "query") {
			body = map[string]any{"status": "success", "data": map[string]any{"result": []any{map[string]any{"value": []any{1, "4"}}}}}
		}
		if strings.Contains(r.URL.Path, "health") {
			body = map[string]any{"database": "ok"}
		}
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{}}, nil
	})}
	got, err := c.snapshot(context.Background())
	if err != nil || got.State != "running" || len(got.Targets) != 19 {
		t.Fatalf("%#v %v", got, err)
	}
}
