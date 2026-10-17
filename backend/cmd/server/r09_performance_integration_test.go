//go:build integration

package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
)

// The R09 contract is deliberately frozen in the test as well as in the
// acceptance record. A is the existing path reference, B adds protection to
// that path, and C adds the versioned body cache to the same protection.
const (
	r09Workers             = 16
	r09RequestsPerWorker   = 32
	r09WarmupPerClient     = 2
	r09OverloadRequests    = 96
	r09ClientTimeout       = 5 * time.Second
	r09P95Limit            = 50 * time.Millisecond
	r09P99Limit            = 100 * time.Millisecond
	r09ProtectedGoodputMin = 0.90
	r09NewPathGoodputMin   = 0.95
	r09DBWorkMaxRatio      = 0.95
	r09MaxNormalRejectRate = 0.01
)

type r09Variant struct {
	name         string
	rateLimit    bool
	bodyV2       bool
	goodputFloor float64
}

type r09Window struct {
	name          string
	total         int
	success       int
	statusCounts  map[int]int
	latencies     []time.Duration
	elapsed       time.Duration
	goodput       float64
	p95           time.Duration
	p99           time.Duration
	rejectionRate float64
	errorRate     float64
	db            r09DatabaseDelta
	metrics       r09MetricEvidence
}

type r09DatabaseDelta struct {
	Questions int64
	ComSelect int64
	RowsRead  int64
	RowsSent  int64
	BytesSent int64
}

type r09DatabaseStatus struct {
	Questions int64
	ComSelect int64
	RowsRead  int64
	RowsSent  int64
	BytesSent int64
}

type r09MetricEvidence struct {
	dbOperations       int64
	dbOperationSeconds float64
	dbByOperation      map[string]int64
	cacheHits          int64
	cacheMisses        int64
	cacheErrors        int64
	rateLimitAllows    int64
	rateLimitDenies    int64
	resourceRejects    int64
}

type r09RequestResult struct {
	status  int
	elapsed time.Duration
	err     error
}

// TestIntegrationRedisReadProtectionR09 compares all three candidates under
// one real dependency set and one fixed HTTP workload. It is intentionally
// separate from read_cost_baseline_integration_test.go: that test is the old
// path's Phase 1 evidence and cannot establish a new-vs-old result.
func TestIntegrationRedisReadProtectionR09(t *testing.T) {
	base := integrationEnvironment(t)
	setEnvironment(&base, "BACKEND_TRUSTED_PROXY_CIDRS", "127.0.0.1/32")
	setEnvironment(&base, "BACKEND_HTTP_MAX_CONCURRENCY", "128")
	setEnvironment(&base, "MYSQL_MAX_OPEN_CONNS", "8")
	setEnvironment(&base, "REDIS_POOL_SIZE", "16")
	setEnvironment(&base, "REDIS_OPERATION_TIMEOUT", "200ms")
	setEnvironment(&base, "REDIS_POOL_TIMEOUT", "200ms")
	setEnvironment(&base, "BACKEND_RESOURCE_READ", "32")
	setEnvironment(&base, "BACKEND_RESOURCE_DATABASE", "32")
	setEnvironment(&base, "BACKEND_RESOURCE_REBUILD", "8")
	setEnvironment(&base, "BACKEND_RESOURCE_WAITER", "256")
	setEnvironment(&base, "BACKEND_RATE_LIMIT_OPERATION_TIMEOUT", "200ms")

	backendDir := findBackendDir(t)
	runIntegrationCommand(t, backendDir, integrationCommandTimeout, base, "go", "run", "./cmd/migrate", "up")

	cfg := r09Config(t, base)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("open R09 database: %v", err)
	}
	defer database.Close()
	releaseFactsLock := integrationtest.AcquirePostFactsLock(t, database)
	defer releaseFactsLock()

	defer componentmetrics.InstallBackend(nil)
	results := make(map[string]r09Window)
	variants := []r09Variant{
		{name: "A_old", rateLimit: false, bodyV2: false, goodputFloor: 0},
		{name: "B_protected_old", rateLimit: true, bodyV2: false, goodputFloor: r09ProtectedGoodputMin},
		{name: "C_protected_body_v2", rateLimit: true, bodyV2: true, goodputFloor: r09NewPathGoodputMin},
	}
	for _, variant := range variants {
		result := runR09Variant(t, base, database, variant)
		results[variant.name] = result
		t.Logf("R09 %s warm_hotspot: total=%d success=%d goodput=%.1f/s p95=%s p99=%s rejection=%.2f%% errors=%.2f%% db_questions=%d db_select=%d rows_read=%d rows_sent=%d bytes_sent=%d db_ops=%d cache_hit=%d cache_miss=%d rate_allow=%d rate_deny=%d resource_reject=%d", variant.name, result.total, result.success, result.goodput, result.p95, result.p99, result.rejectionRate*100, result.errorRate*100, result.db.Questions, result.db.ComSelect, result.db.RowsRead, result.db.RowsSent, result.db.BytesSent, result.metrics.dbOperations, result.metrics.cacheHits, result.metrics.cacheMisses, result.metrics.rateLimitAllows, result.metrics.rateLimitDenies, result.metrics.resourceRejects)
	}

	assertR09Contract(t, results)
}

func runR09Variant(t *testing.T, base []string, database *sql.DB, variant r09Variant) r09Window {
	t.Helper()
	environment := append([]string(nil), base...)
	setEnvironment(&environment, "BACKEND_RATE_LIMIT_ENABLED", strconv.FormatBool(variant.rateLimit))
	setEnvironment(&environment, "BACKEND_POST_BODY_V2_ENABLED", strconv.FormatBool(variant.bodyV2))

	suffix := strconv.FormatInt(time.Now().UnixNano()%1000000000, 10)
	instanceName := strings.ReplaceAll(strings.ToLower(variant.name), "_", "-")
	server1 := startProtectionAssembly(t, environment, "r09-"+instanceName+"-1", "")
	server2 := startProtectionAssembly(t, environment, "r09-"+instanceName+"-2", "")

	clients, userIDs := r09Clients(t, server1.address, suffix, variant.rateLimit)
	postID := createProtectionPost(t, clients[0], server1.address, "r09 post "+suffix, strings.Repeat("r09 body ", 8))
	cleaned := false
	cleanup := func() {
		if cleaned {
			return
		}
		cleaned = true
		cleanupR09Fixture(t, database, userIDs, postID)
	}
	t.Cleanup(cleanup)

	// The first window is a cold, same-post probe. It is evidence for the
	// contract's cold branch; the frozen pass/fail comparison uses the larger
	// warm hotspot window below.
	installR09Metrics(t)
	coldBefore := r09ReadDatabaseStatus(t, database)
	cold := runR09Window(t, "cold_hotspot", clients, server1, server2, postID, 1)
	coldAfter := r09ReadDatabaseStatus(t, database)
	cold.db = coldAfter.delta(coldBefore)
	cold.metrics = r09MetricEvidenceFromMetrics(t, componentmetrics.BackendActive())
	if variant.rateLimit {
		if cold.p95 > r09P95Limit || cold.p99 > r09P99Limit {
			t.Errorf("R09 %s cold tail latency p95=%s p99=%s exceeds %s/%s", variant.name, cold.p95, cold.p99, r09P95Limit, r09P99Limit)
		}
		if cold.rejectionRate > r09MaxNormalRejectRate {
			t.Errorf("R09 %s cold rejection rate %.2f%% exceeds %.2f%%", variant.name, cold.rejectionRate*100, r09MaxNormalRejectRate*100)
		}
	}
	t.Logf("R09 %s cold_hotspot: total=%d success=%d goodput=%.1f/s p95=%s p99=%s rejection=%.2f%% db_questions=%d db_ops=%d cache_hit=%d cache_miss=%d", variant.name, cold.total, cold.success, cold.goodput, cold.p95, cold.p99, cold.rejectionRate*100, cold.db.Questions, cold.metrics.dbOperations, cold.metrics.cacheHits, cold.metrics.cacheMisses)

	for index, client := range clients {
		address := server1.address
		if index%2 == 1 {
			address = server2.address
		}
		for warmup := 0; warmup < r09WarmupPerClient; warmup++ {
			status, _, err := r09Read(client, address, postID)
			if err != nil || status != http.StatusOK {
				t.Fatalf("R09 %s warmup status=%d error=%v", variant.name, status, err)
			}
		}
	}

	metrics := installR09Metrics(t)
	before := r09ReadDatabaseStatus(t, database)
	warm := runR09Window(t, "warm_hotspot", clients, server1, server2, postID, r09RequestsPerWorker)
	after := r09ReadDatabaseStatus(t, database)
	warm.db = after.delta(before)
	warm.metrics = r09MetricEvidenceFromMetrics(t, metrics)

	if variant.rateLimit {
		overloadMetrics := installR09Metrics(t)
		overload := runR09Window(t, "protected_overload", clients[:1], server1, server2, postID, r09OverloadRequests)
		overload.metrics = r09MetricEvidenceFromMetrics(t, overloadMetrics)
		if overload.statusCounts[http.StatusTooManyRequests] == 0 {
			t.Errorf("R09 %s overload produced no 429 responses: statuses=%v", variant.name, overload.statusCounts)
		}
		t.Logf("R09 %s protected_overload: total=%d success=%d p95=%s p99=%s rejection=%.2f%% statuses=%v rate_deny=%d resource_reject=%d", variant.name, overload.total, overload.success, overload.p95, overload.p99, overload.rejectionRate*100, overload.statusCounts, overload.metrics.rateLimitDenies, overload.metrics.resourceRejects)
	}

	server1.close()
	server2.close()
	cleanup()
	return warm
}

func r09Clients(t *testing.T, address, suffix string, protected bool) ([]*http.Client, []uint64) {
	t.Helper()
	clients := make([]*http.Client, r09Workers)
	userIDs := make([]uint64, r09Workers)
	for index := range clients {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("create R09 cookie jar: %v", err)
		}
		client := &http.Client{Jar: jar, Timeout: r09ClientTimeout}
		username := fmt.Sprintf("R09_%s_%02d", suffix, index)
		userIDs[index] = registerR09User(t, client, address, username, "r09-performance-password", index, protected)
		clients[index] = client
	}
	return clients, userIDs
}

func registerR09User(t *testing.T, client *http.Client, address, username, password string, index int, protected bool) uint64 {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatalf("encode R09 registration: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, address+"/api/v1/auth/register", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("create R09 registration request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if protected {
		request.Header.Set("X-Forwarded-For", fmt.Sprintf("198.18.0.%d", index+1))
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("register R09 user: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register R09 user status=%d body=%s", response.StatusCode, readIntegrationBody(response))
	}
	var result struct {
		Data struct {
			ID uint64 `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode R09 registration: %v", err)
	}
	if result.Data.ID == 0 {
		t.Fatal("R09 registration returned no user ID")
	}
	return result.Data.ID
}

func runR09Window(t *testing.T, name string, clients []*http.Client, server1, server2 *protectionInstance, postID uint64, requestsPerWorker int) r09Window {
	t.Helper()
	if len(clients) == 0 || requestsPerWorker <= 0 {
		t.Fatalf("invalid R09 window clients=%d requests_per_worker=%d", len(clients), requestsPerWorker)
	}
	results := make(chan r09RequestResult, len(clients)*requestsPerWorker)
	start := make(chan struct{})
	started := time.Now()
	var group sync.WaitGroup
	for worker, client := range clients {
		group.Add(1)
		go func(worker int, client *http.Client) {
			defer group.Done()
			<-start
			for requestIndex := 0; requestIndex < requestsPerWorker; requestIndex++ {
				address := server1.address
				if (worker+requestIndex)%2 == 1 {
					address = server2.address
				}
				status, elapsed, err := r09Read(client, address, postID)
				results <- r09RequestResult{status: status, elapsed: elapsed, err: err}
			}
		}(worker, client)
	}
	close(start)
	group.Wait()
	close(results)

	window := r09Window{name: name, total: len(clients) * requestsPerWorker, statusCounts: make(map[int]int)}
	for result := range results {
		window.statusCounts[result.status]++
		if result.err != nil {
			window.errorRate++
			continue
		}
		if result.status == http.StatusOK {
			window.success++
			window.latencies = append(window.latencies, result.elapsed)
		}
		if result.status == http.StatusTooManyRequests || result.status == http.StatusServiceUnavailable {
			window.rejectionRate++
		}
	}
	window.elapsed = time.Since(started)
	if window.elapsed > 0 {
		window.goodput = float64(window.success) / window.elapsed.Seconds()
	}
	window.rejectionRate /= float64(window.total)
	window.errorRate /= float64(window.total)
	window.p95 = r09Percentile(window.latencies, 0.95)
	window.p99 = r09Percentile(window.latencies, 0.99)
	return window
}

func r09Read(client *http.Client, address string, postID uint64) (int, time.Duration, error) {
	started := time.Now()
	request, err := http.NewRequest(http.MethodGet, address+"/api/v1/posts/"+strconv.FormatUint(postID, 10), nil)
	if err != nil {
		return 0, time.Since(started), err
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, time.Since(started), err
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
	closeErr := response.Body.Close()
	if readErr != nil {
		return response.StatusCode, time.Since(started), readErr
	}
	return response.StatusCode, time.Since(started), closeErr
}

func r09Percentile(values []time.Duration, percentile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left] < sorted[right] })
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func installR09Metrics(t *testing.T) *componentmetrics.Backend {
	t.Helper()
	metrics, err := componentmetrics.NewBackend(componentmetrics.BackendRoutes())
	if err != nil {
		t.Fatalf("create R09 metrics: %v", err)
	}
	metrics.ObserveOutbox(0, 0, nil)
	componentmetrics.InstallBackend(metrics)
	return metrics
}

func r09MetricEvidenceFromMetrics(t *testing.T, metrics *componentmetrics.Backend) r09MetricEvidence {
	t.Helper()
	if metrics == nil {
		t.Fatal("R09 metrics are nil")
	}
	payload, ok := metrics.Snapshot()
	if !ok {
		t.Fatal("R09 metrics snapshot is unavailable")
	}
	evidence := r09MetricEvidence{dbByOperation: make(map[string]int64)}
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	for scanner.Scan() {
		name, labels, value, ok := parseR09MetricLine(scanner.Text())
		if !ok {
			continue
		}
		switch name {
		case "gopulse_backend_db_operations_total":
			evidence.dbOperations += int64(value)
			operation := labels["operation"]
			evidence.dbByOperation[operation] += int64(value)
		case "gopulse_backend_db_operation_duration_seconds_total":
			evidence.dbOperationSeconds += value
		case "gopulse_backend_cache_requests_total":
			switch labels["result"] {
			case "hit":
				evidence.cacheHits += int64(value)
			case "miss":
				evidence.cacheMisses += int64(value)
			case "error":
				evidence.cacheErrors += int64(value)
			}
		case "gopulse_backend_rate_limit_decisions_total":
			switch labels["result"] {
			case "allow":
				evidence.rateLimitAllows += int64(value)
			case "deny":
				evidence.rateLimitDenies += int64(value)
			}
		case "gopulse_backend_resource_rejected_total":
			evidence.resourceRejects += int64(value)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan R09 metrics: %v", err)
	}
	return evidence
}

func parseR09MetricLine(line string) (string, map[string]string, float64, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil, 0, false
	}
	parts := strings.Fields(line)
	if len(parts) != 2 {
		return "", nil, 0, false
	}
	value, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return "", nil, 0, false
	}
	metric := parts[0]
	labels := make(map[string]string)
	if opening := strings.IndexByte(metric, '{'); opening >= 0 && strings.HasSuffix(metric, "}") {
		labelText := strings.TrimSuffix(metric[opening+1:], "}")
		metric = metric[:opening]
		for _, item := range strings.Split(labelText, ",") {
			key, value, found := strings.Cut(item, "=")
			if !found {
				continue
			}
			labels[key] = strings.Trim(value, "\"")
		}
	}
	return metric, labels, value, true
}

func r09Config(t *testing.T, environment []string) config.Config {
	t.Helper()
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	cfg, err := config.LoadFrom(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("load R09 configuration: %v", err)
	}
	return cfg
}

func r09ReadDatabaseStatus(t *testing.T, database *sql.DB) r09DatabaseStatus {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := database.QueryContext(ctx, `SHOW GLOBAL STATUS WHERE Variable_name IN ('Questions', 'Com_select', 'Rows_read', 'Rows_sent', 'Bytes_sent')`)
	if err != nil {
		t.Fatalf("read R09 MySQL status: %v", err)
	}
	defer rows.Close()
	status := r09DatabaseStatus{}
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			t.Fatalf("scan R09 MySQL status: %v", err)
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			t.Fatalf("parse R09 MySQL status %s=%q: %v", name, raw, err)
		}
		switch name {
		case "Questions":
			status.Questions = value
		case "Com_select":
			status.ComSelect = value
		case "Rows_read":
			status.RowsRead = value
		case "Rows_sent":
			status.RowsSent = value
		case "Bytes_sent":
			status.BytesSent = value
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate R09 MySQL status: %v", err)
	}
	return status
}

func (after r09DatabaseStatus) delta(before r09DatabaseStatus) r09DatabaseDelta {
	return r09DatabaseDelta{
		Questions: after.Questions - before.Questions,
		ComSelect: after.ComSelect - before.ComSelect,
		RowsRead:  after.RowsRead - before.RowsRead,
		RowsSent:  after.RowsSent - before.RowsSent,
		BytesSent: after.BytesSent - before.BytesSent,
	}
}

func cleanupR09Fixture(t *testing.T, database *sql.DB, userIDs []uint64, postID uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{query: "DELETE FROM post_bookmarks WHERE post_id=?", args: []any{postID}},
		{query: "DELETE FROM post_likes WHERE post_id=?", args: []any{postID}},
		{query: "DELETE FROM comments WHERE post_id=?", args: []any{postID}},
		{query: "DELETE FROM posts WHERE id=?", args: []any{postID}},
	} {
		if _, err := database.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Errorf("cleanup R09 fixture: %v", err)
		}
	}
	for _, userID := range userIDs {
		if _, err := database.ExecContext(ctx, "DELETE FROM users WHERE id=?", userID); err != nil {
			t.Errorf("cleanup R09 user %d: %v", userID, err)
		}
	}
}

func assertR09Contract(t *testing.T, results map[string]r09Window) {
	t.Helper()
	baseline, ok := results["A_old"]
	if !ok || baseline.success == 0 || baseline.goodput <= 0 {
		t.Fatalf("R09 old path has no usable warm baseline: %#v", baseline)
	}
	for _, variant := range []r09Variant{
		{name: "B_protected_old", goodputFloor: r09ProtectedGoodputMin},
		{name: "C_protected_body_v2", goodputFloor: r09NewPathGoodputMin},
	} {
		result, ok := results[variant.name]
		if !ok {
			t.Fatalf("R09 result %s is missing", variant.name)
		}
		if result.p95 > r09P95Limit || result.p99 > r09P99Limit {
			t.Errorf("R09 %s tail latency p95=%s p99=%s exceeds %s/%s", variant.name, result.p95, result.p99, r09P95Limit, r09P99Limit)
		}
		if result.goodput < baseline.goodput*variant.goodputFloor {
			t.Errorf("R09 %s goodput %.1f/s is below %.0f%% of old %.1f/s", variant.name, result.goodput, variant.goodputFloor*100, baseline.goodput)
		}
		if result.rejectionRate > r09MaxNormalRejectRate {
			t.Errorf("R09 %s normal rejection rate %.2f%% exceeds %.2f%%", variant.name, result.rejectionRate*100, r09MaxNormalRejectRate*100)
		}
	}
	newPath := results["C_protected_body_v2"]
	oldWork := float64(baseline.db.Questions) / float64(baseline.success)
	newWork := float64(newPath.db.Questions) / float64(newPath.success)
	if oldWork <= 0 || newWork > oldWork*r09DBWorkMaxRatio {
		t.Errorf("R09 new body path database work %.3f queries/success exceeds old %.3f with %.0f%% target", newWork, oldWork, (1-r09DBWorkMaxRatio)*100)
	}
}
