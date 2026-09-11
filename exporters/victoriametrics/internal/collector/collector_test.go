package collector

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fixture() string {
	var b strings.Builder
	for _, m := range mappings {
		for _, v := range m.values {
			fmt.Fprintf(&b, "%s{%s=%q} 1\n", m.upstream, m.label, v)
		}
	}
	return b.String() + "gopulse_fake_recursion 999\nvm_http_requests_total{path=\"/metrics\"} 999\nvm_rows{type=\"indexdb/file\"} 999\n"
}
func TestSnapshotContract(t *testing.T) {
	good := fixture()
	body, err := Snapshot(good)
	if err != nil || strings.Count(body, "# TYPE ") != 9 {
		t.Fatalf("incomplete snapshot: %v", err)
	}
	for _, want := range []string{"rows_inserted_total 16\n", "query_requests_total 2\n", "active_timeseries 1\n", "storage_rows 3\n", "storage_size_bytes 7\n", "active_merges 5\n", "storage_rows_deleted_total 3\n"} {
		if !strings.Contains(body, "gopulse_victoriametrics_"+want) {
			t.Fatal(want)
		}
	}
	if strings.Contains(body, "{") || strings.Contains(body, "999") || strings.Contains(body, "retention") {
		t.Fatal("unmanaged output")
	}
	selected := "vm_rows_deleted_total{type=\"storage/small\"} 1\n"
	for name, bad := range map[string]string{
		"missing":   strings.Replace(good, selected, "", 1),
		"duplicate": good + selected,
		"nonfinite": strings.Replace(good, selected, strings.Replace(selected, " 1", " NaN", 1), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if body, err := Snapshot(bad); err == nil || body != "" {
				t.Fatal("partial or invalid snapshot accepted")
			}
		})
	}
}
func TestAuthenticatedFailureAndRecovery(t *testing.T) {
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if r.URL.RequestURI() != "/metrics" || r.Method != "GET" {
			t.Error("unexpected request")
		}
		if !ok || u != "user" || p != "private-canary" || fail {
			w.WriteHeader(401)
			w.Write([]byte("private-canary"))
			return
		}
		w.Write([]byte(fixture()))
	}))
	defer server.Close()
	c := &Client{HTTP: server.Client(), Origin: server.URL, Username: "user", Password: "private-canary"}
	for _, broken := range []bool{false, true, false} {
		fail = broken
		body, err := Collect(context.Background(), c)
		if broken {
			if err != ErrUnavailable || body != "" {
				t.Fatal("unsafe failure")
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}
