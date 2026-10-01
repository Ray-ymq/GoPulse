package retention

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu           sync.Mutex
	cluster      string
	indices      []Index
	ownership    map[string]bool
	inspectCalls []string
	blocked      []string
	deleted      []string
	deleteErrors []error
	blockErrors  []error
}

func (f *fakeStore) ClusterIdentity(context.Context) (string, error) { return f.cluster, nil }
func (f *fakeStore) List(context.Context) ([]Index, error) {
	return append([]Index(nil), f.indices...), nil
}
func (f *fakeStore) Inspect(_ context.Context, index string, _ Policy, _ string) (Ownership, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inspectCalls = append(f.inspectCalls, index)
	return Ownership{Valid: f.ownership[index], ClusterUUID: f.cluster}, nil
}
func (f *fakeStore) BlockWrites(_ context.Context, index string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blocked = append(f.blocked, index)
	if len(f.blockErrors) > 0 {
		err := f.blockErrors[0]
		f.blockErrors = f.blockErrors[1:]
		return err
	}
	return nil
}
func (f *fakeStore) DeleteIndex(_ context.Context, index string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, index)
	if len(f.deleteErrors) > 0 {
		err := f.deleteErrors[0]
		f.deleteErrors = f.deleteErrors[1:]
		return err
	}
	return nil
}

func testRunner(t *testing.T, store *fakeStore) *Runner {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Cycle = 10 * time.Second
	cfg.RoundTimeout = time.Second
	cfg.RequestTimeout = 100 * time.Millisecond
	cfg.RetryMin = 10 * time.Millisecond
	cfg.RetryMax = 100 * time.Millisecond
	cfg.MaxRetries = 2
	runner, err := NewRunner(store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	runner.now = func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }
	runner.sleep = func(context.Context, time.Duration) error { return nil }
	return runner
}

func TestRunnerDeletesOnlyOwnedExpiredIndicesAndBlocksFirst(t *testing.T) {
	store := &fakeStore{
		cluster: "cluster-a",
		indices: []Index{
			{Name: "gopulse-logs-v1-2026.09.20"},
			{Name: "gopulse-logs-v1-2026.09.24"},
			{Name: "gopulse-logs-v1-2026.02.30"},
			{Name: "gopulse-post-search-v1-2026.09.20"},
			{Name: "gopulse-events-v1-2026.09.20"},
		},
		ownership: map[string]bool{"gopulse-logs-v1-2026.09.20": true, "gopulse-events-v1-2026.09.20": false},
	}
	report, err := testRunner(t, store).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(store.blocked) != 1 || len(store.deleted) != 1 || store.blocked[0] != store.deleted[0] {
		t.Fatalf("blocked=%v deleted=%v", store.blocked, store.deleted)
	}
	var reasons = map[string]string{}
	for _, item := range report.Items {
		reasons[item.Index] = item.Result
	}
	if reasons["gopulse-logs-v1-2026.09.20"] != "deleted" || reasons["gopulse-logs-v1-2026.09.24"] != "not_expired" || reasons["gopulse-logs-v1-2026.02.30"] != "invalid_date" || reasons["gopulse-post-search-v1-2026.09.20"] != "not_owned" || reasons["gopulse-events-v1-2026.09.20"] != "not_owned" {
		t.Fatalf("unexpected report results: %v", reasons)
	}
}

func TestRunnerRetriesTransientDeleteAndDoesNotHidePermissionFailure(t *testing.T) {
	store := &fakeStore{
		cluster:      "cluster-a",
		indices:      []Index{{Name: "gopulse-logs-v1-2026.09.20"}},
		ownership:    map[string]bool{"gopulse-logs-v1-2026.09.20": true},
		deleteErrors: []error{&HTTPError{Status: http.StatusServiceUnavailable, Op: "delete"}},
	}
	report, err := testRunner(t, store).RunOnce(context.Background())
	if err != nil || len(store.deleted) != 2 {
		t.Fatalf("err=%v deleted=%v", err, store.deleted)
	}
	if report.Items[0].Result != "deleted" || report.Items[0].Attempts != 2 {
		t.Fatalf("item=%+v", report.Items[0])
	}

	store = &fakeStore{cluster: "cluster-a", indices: []Index{{Name: "gopulse-logs-v1-2026.09.20"}}, ownership: map[string]bool{"gopulse-logs-v1-2026.09.20": true}, deleteErrors: []error{&HTTPError{Status: http.StatusForbidden, Op: "delete"}}}
	report, err = testRunner(t, store).RunOnce(context.Background())
	if err != nil || report.Items[0].Result != "permission_denied" || len(store.deleted) != 1 {
		t.Fatalf("permission report=%+v err=%v", report.Items[0], err)
	}
}

func TestRunnerDoesNotDeleteWhenClusterChanges(t *testing.T) {
	store := &fakeStore{cluster: "cluster-a", indices: []Index{{Name: "gopulse-logs-v1-2026.09.20"}}, ownership: map[string]bool{"gopulse-logs-v1-2026.09.20": true}}
	runner := testRunner(t, store)
	runner.cluster = "cluster-a"
	store.cluster = "cluster-b"
	report, err := runner.RunOnce(context.Background())
	if err == nil || report.Err == "" || len(store.deleted) != 0 {
		t.Fatalf("report=%+v err=%v deleted=%v", report, err, store.deleted)
	}
	if !errors.Is(err, errors.New("Elasticsearch cluster identity changed")) && err.Error() != "Elasticsearch cluster identity changed" {
		t.Fatalf("unexpected error: %v", err)
	}
}
