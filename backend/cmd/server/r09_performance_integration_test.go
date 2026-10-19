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
	"net"
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
	goredis "github.com/redis/go-redis/v9"
)

// The R09 contract is deliberately frozen in the test as well as in the
// acceptance record. Both variants use the current body cache; B adds quota
// protection to A. The historical v1/v2 comparison is preserved in Git.
const (
	r09Workers           = 16
	r09RequestsPerWorker = 32
	r09WarmupPerClient   = 2
	r09OverloadRequests  = 96
	r09UniformPostCount  = 16
	r09NormalSamples     = 3
	r09ClientTimeout     = 5 * time.Second
	r09P95Limit          = 50 * time.Millisecond
	r09P99Limit          = 100 * time.Millisecond
	r09FaultP95Limit     = time.Second
	r09FaultP99Limit     = 2 * time.Second
	// Normal windows repeat three times; the contract compares their median
	// effective goodput while aggregating tail and database evidence.
	// The protected path adds one shared Redis decision per request. Its
	// budget compares protected and unprotected reads using the same body cache.
	r09ProtectedGoodputMin             = 0.90
	r09DBWorkMaxRatio                  = 0.95
	r09MaxNormalRejectRate             = 0.01
	r09PoolWaitDurationPerSuccessLimit = 5 * time.Millisecond
	r09PoolWaitAvgLimit                = 5 * time.Millisecond
	r09ExpectedPoolMaxOpen             = 8
)

type r09AccessPattern string

const (
	r09HotspotPattern r09AccessPattern = "hotspot"
	r09UniformPattern r09AccessPattern = "uniform"
)

type r09Variant struct {
	name      string
	rateLimit bool
}

type r09Window struct {
	name           string
	pattern        r09AccessPattern
	total          int
	success        int
	errors         int
	statusCounts   map[int]int
	latencies      []time.Duration
	elapsed        time.Duration
	goodput        float64
	goodputSamples []float64
	p95            time.Duration
	p99            time.Duration
	rejectionRate  float64
	errorRate      float64
	db             r09DatabaseDelta
	metrics        r09MetricEvidence
	redis          r09RedisDelta
	pool           r09PoolEvidence
}

type r09VariantResult struct {
	coldHotspot     r09Window
	coldUniform     r09Window
	warmHotspot     r09Window
	warmUniform     r09Window
	faultHotspot    r09Window
	faultUniform    r09Window
	recoveryHotspot r09Window
	recoveryUniform r09Window
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

type r09RedisCommandEvidence struct {
	calls int64
	usec  int64
}

type r09RedisStatus struct {
	commands map[string]r09RedisCommandEvidence
}

type r09RedisDelta struct {
	evalCalls    int64
	evalUsec     int64
	evalSHACalls int64
	evalSHAUsec  int64
	getCalls     int64
	getUsec      int64
	setCalls     int64
	setUsec      int64
	delCalls     int64
	delUsec      int64
}

type r09RequestResult struct {
	status  int
	elapsed time.Duration
	err     error
}

type r09PoolEvidence struct {
	instances    map[string]r09PoolInstanceEvidence
	waitCount    int64
	waitDuration time.Duration
}

type r09PoolInstanceEvidence struct {
	maxOpen      int
	open         int
	inUse        int
	idle         int
	waitCount    int64
	waitDuration time.Duration
}

type r09PoolSnapshot struct {
	instances map[string]r09PoolInstanceEvidence
}

type r09BusinessBudget struct {
	epoch          string
	open           uint64
	retainedGroups uint64
	retainedBytes  uint64
	parkedMessages uint64
	parkedBytes    uint64
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
	setEnvironment(&base, "MYSQL_MAX_IDLE_CONNS", "8")
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
	results := make(map[string]r09VariantResult)
	variants := []r09Variant{
		{name: "A_body", rateLimit: false},
		{name: "B_protected_body", rateLimit: true},
	}
	for _, variant := range variants {
		result := runR09Variant(t, base, cfg, database, variant)
		results[variant.name] = result
		logR09Window(t, variant.name, result.warmHotspot)
		logR09Window(t, variant.name, result.warmUniform)
		logR09Window(t, variant.name, result.faultHotspot)
		logR09Window(t, variant.name, result.faultUniform)
		logR09Window(t, variant.name, result.recoveryHotspot)
		logR09Window(t, variant.name, result.recoveryUniform)
	}

	assertR09Contract(t, results)
}

func runR09Variant(t *testing.T, base []string, cfg config.Config, database *sql.DB, variant r09Variant) r09VariantResult {
	t.Helper()
	environment := append([]string(nil), base...)
	setEnvironment(&environment, "BACKEND_RATE_LIMIT_ENABLED", strconv.FormatBool(variant.rateLimit))

	suffix := strconv.FormatInt(time.Now().UnixNano()%1000000000, 10)
	instanceName := strings.ReplaceAll(strings.ToLower(variant.name), "_", "-")
	server1 := startProtectionAssembly(t, environment, "r09-"+instanceName+"-1", "")
	server2 := startProtectionAssembly(t, environment, "r09-"+instanceName+"-2", "")
	budget := r09ReadBusinessBudget(t, database)

	clients, userIDs := r09Clients(t, server1.address, suffix, variant.rateLimit)
	postIDs := make([]uint64, 0, r09UniformPostCount+1)
	postIDs = append(postIDs, createProtectionPost(t, clients[0], server1.address, "r09 hotspot "+suffix, strings.Repeat("r09 body ", 8)))
	for index := 0; index < r09UniformPostCount; index++ {
		postIDs = append(postIDs, createProtectionPost(t, clients[0], server1.address, fmt.Sprintf("r09 uniform %s %02d", suffix, index), strings.Repeat("r09 body ", 8)))
	}
	cleaned := false
	cleanup := func() {
		if cleaned {
			return
		}
		cleaned = true
		cleanupR09Fixture(t, database, userIDs, postIDs, budget)
	}
	t.Cleanup(cleanup)
	result := r09VariantResult{}

	// Cold windows deliberately use disjoint keys: postIDs[0] is the hotspot,
	// while postIDs[1:] are the uniform working set. This keeps both access
	// patterns genuinely cold without flushing unrelated Redis data.
	installR09Metrics(t)
	coldBefore := r09ReadDatabaseStatus(t, database)
	coldHotspot := runR09Window(t, "cold_hotspot", r09HotspotPattern, clients, server1, server2, postIDs, 1)
	coldAfter := r09ReadDatabaseStatus(t, database)
	coldHotspot.db = coldAfter.delta(coldBefore)
	coldHotspot.metrics = r09MetricEvidenceFromMetrics(t, componentmetrics.BackendActive())
	if variant.rateLimit {
		if coldHotspot.p95 > r09P95Limit || coldHotspot.p99 > r09P99Limit {
			t.Errorf("R09 %s cold hotspot tail latency p95=%s p99=%s exceeds %s/%s", variant.name, coldHotspot.p95, coldHotspot.p99, r09P95Limit, r09P99Limit)
		}
		if coldHotspot.rejectionRate > r09MaxNormalRejectRate {
			t.Errorf("R09 %s cold hotspot rejection rate %.2f%% exceeds %.2f%%", variant.name, coldHotspot.rejectionRate*100, r09MaxNormalRejectRate*100)
		}
	}
	result.coldHotspot = coldHotspot
	logR09Window(t, variant.name, coldHotspot)

	installR09Metrics(t)
	coldBefore = r09ReadDatabaseStatus(t, database)
	coldUniform := runR09Window(t, "cold_uniform", r09UniformPattern, clients, server1, server2, postIDs, 1)
	coldAfter = r09ReadDatabaseStatus(t, database)
	coldUniform.db = coldAfter.delta(coldBefore)
	coldUniform.metrics = r09MetricEvidenceFromMetrics(t, componentmetrics.BackendActive())
	result.coldUniform = coldUniform
	logR09Window(t, variant.name, coldUniform)

	for index, client := range clients {
		address := server1.address
		if index%2 == 1 {
			address = server2.address
		}
		for warmup := 0; warmup < r09WarmupPerClient; warmup++ {
			status, _, err := r09Read(client, address, postIDs[0])
			if err != nil || status != http.StatusOK {
				t.Fatalf("R09 %s warmup status=%d error=%v", variant.name, status, err)
			}
		}
		status, _, err := r09Read(client, address, postIDs[index%r09UniformPostCount+1])
		if err != nil || status != http.StatusOK {
			t.Fatalf("R09 %s uniform warmup status=%d error=%v", variant.name, status, err)
		}
	}

	result.warmHotspot = runR09NormalSamples(t, "warm_hotspot", r09HotspotPattern, clients, server1, server2, postIDs, database, cfg, userIDs, variant.rateLimit)

	if variant.rateLimit {
		overloadMetrics := installR09Metrics(t)
		resetR09RateLimitBuckets(t, cfg, userIDs)
		overload := runR09Window(t, "protected_overload", r09HotspotPattern, clients[:1], server1, server2, postIDs, r09OverloadRequests)
		overload.metrics = r09MetricEvidenceFromMetrics(t, overloadMetrics)
		if overload.statusCounts[http.StatusTooManyRequests] == 0 {
			t.Errorf("R09 %s overload produced no 429 responses: statuses=%v", variant.name, overload.statusCounts)
		}
		logR09Window(t, variant.name, overload)
	}

	result.warmUniform = runR09NormalSamples(t, "warm_uniform", r09UniformPattern, clients, server1, server2, postIDs, database, cfg, userIDs, variant.rateLimit)

	server1.close()
	server2.close()

	broken1 := startProtectionAssembly(t, environment, "r09-"+instanceName+"-broken-1", "1")
	broken2 := startProtectionAssembly(t, environment, "r09-"+instanceName+"-broken-2", "1")
	result.faultHotspot = runR09MeasuredWindow(t, "redis_fault_hotspot", r09HotspotPattern, clients, broken1, broken2, postIDs, database, cfg, userIDs, variant.rateLimit)
	result.faultUniform = runR09MeasuredWindow(t, "redis_fault_uniform", r09UniformPattern, clients, broken1, broken2, postIDs, database, cfg, userIDs, variant.rateLimit)
	broken1.close()
	broken2.close()

	recovered1 := startProtectionAssembly(t, environment, "r09-"+instanceName+"-recovered-1", "")
	recovered2 := startProtectionAssembly(t, environment, "r09-"+instanceName+"-recovered-2", "")
	for index, client := range clients {
		address := recovered1.address
		if index%2 == 1 {
			address = recovered2.address
		}
		for _, postID := range []uint64{postIDs[0], postIDs[index%r09UniformPostCount+1]} {
			status, _, err := r09Read(client, address, postID)
			if err != nil || status != http.StatusOK {
				t.Fatalf("R09 %s recovery warmup status=%d error=%v", variant.name, status, err)
			}
		}
	}
	result.recoveryHotspot = runR09MeasuredWindow(t, "redis_recovery_hotspot", r09HotspotPattern, clients, recovered1, recovered2, postIDs, database, cfg, userIDs, variant.rateLimit)
	result.recoveryUniform = runR09MeasuredWindow(t, "redis_recovery_uniform", r09UniformPattern, clients, recovered1, recovered2, postIDs, database, cfg, userIDs, variant.rateLimit)
	recovered1.close()
	recovered2.close()

	cleanup()
	return result
}

func runR09MeasuredWindow(t *testing.T, name string, pattern r09AccessPattern, clients []*http.Client, server1, server2 *protectionInstance, postIDs []uint64, database *sql.DB, cfg config.Config, userIDs []uint64, protected bool) r09Window {
	t.Helper()
	if protected {
		resetR09RateLimitBuckets(t, cfg, userIDs)
	}
	metrics := installR09Metrics(t)
	before := r09ReadDatabaseStatus(t, database)
	window := runR09Window(t, name, pattern, clients, server1, server2, postIDs, r09RequestsPerWorker)
	after := r09ReadDatabaseStatus(t, database)
	window.db = after.delta(before)
	window.metrics = r09MetricEvidenceFromMetrics(t, metrics)
	return window
}

func runR09NormalSamples(t *testing.T, name string, pattern r09AccessPattern, clients []*http.Client, server1, server2 *protectionInstance, postIDs []uint64, database *sql.DB, cfg config.Config, userIDs []uint64, protected bool) r09Window {
	t.Helper()
	var aggregate r09Window
	for sample := 0; sample < r09NormalSamples; sample++ {
		if protected {
			resetR09RateLimitBuckets(t, cfg, userIDs)
		}
		metrics := installR09Metrics(t)
		before := r09ReadDatabaseStatus(t, database)
		redisBefore := r09ReadRedisStatus(t, cfg)
		window := runR09Window(t, name, pattern, clients, server1, server2, postIDs, r09RequestsPerWorker)
		redisAfter := r09ReadRedisStatus(t, cfg)
		after := r09ReadDatabaseStatus(t, database)
		window.db = after.delta(before)
		window.metrics = r09MetricEvidenceFromMetrics(t, metrics)
		window.redis = redisAfter.delta(redisBefore)
		aggregate = mergeR09Windows(aggregate, window)
	}
	return aggregate
}

func mergeR09Windows(aggregate, current r09Window) r09Window {
	if aggregate.total == 0 {
		current.goodputSamples = []float64{current.goodput}
		return current
	}
	aggregate.total += current.total
	aggregate.success += current.success
	aggregate.errors += current.errors
	for status, count := range current.statusCounts {
		aggregate.statusCounts[status] += count
	}
	aggregate.latencies = append(aggregate.latencies, current.latencies...)
	aggregate.elapsed += current.elapsed
	aggregate.db.Questions += current.db.Questions
	aggregate.db.ComSelect += current.db.ComSelect
	aggregate.db.RowsRead += current.db.RowsRead
	aggregate.db.RowsSent += current.db.RowsSent
	aggregate.db.BytesSent += current.db.BytesSent
	aggregate.metrics.dbOperations += current.metrics.dbOperations
	aggregate.metrics.dbOperationSeconds += current.metrics.dbOperationSeconds
	aggregate.metrics.cacheHits += current.metrics.cacheHits
	aggregate.metrics.cacheMisses += current.metrics.cacheMisses
	aggregate.metrics.cacheErrors += current.metrics.cacheErrors
	aggregate.metrics.rateLimitAllows += current.metrics.rateLimitAllows
	aggregate.metrics.rateLimitDenies += current.metrics.rateLimitDenies
	aggregate.metrics.resourceRejects += current.metrics.resourceRejects
	aggregate.redis.evalCalls += current.redis.evalCalls
	aggregate.redis.evalUsec += current.redis.evalUsec
	aggregate.redis.evalSHACalls += current.redis.evalSHACalls
	aggregate.redis.evalSHAUsec += current.redis.evalSHAUsec
	aggregate.redis.getCalls += current.redis.getCalls
	aggregate.redis.getUsec += current.redis.getUsec
	aggregate.redis.setCalls += current.redis.setCalls
	aggregate.redis.setUsec += current.redis.setUsec
	aggregate.redis.delCalls += current.redis.delCalls
	aggregate.redis.delUsec += current.redis.delUsec
	for operation, count := range current.metrics.dbByOperation {
		aggregate.metrics.dbByOperation[operation] += count
	}
	for name, currentInstance := range current.pool.instances {
		instance := aggregate.pool.instances[name]
		instance.maxOpen = currentInstance.maxOpen
		instance.open = currentInstance.open
		instance.inUse = currentInstance.inUse
		instance.idle = currentInstance.idle
		instance.waitCount += currentInstance.waitCount
		instance.waitDuration += currentInstance.waitDuration
		aggregate.pool.instances[name] = instance
	}
	aggregate.pool.waitCount += current.pool.waitCount
	aggregate.pool.waitDuration += current.pool.waitDuration
	aggregate.goodputSamples = append(aggregate.goodputSamples, current.goodput)
	aggregate.goodput = r09MedianGoodput(aggregate.goodputSamples)
	aggregate.rejectionRate = 0
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		aggregate.rejectionRate += float64(aggregate.statusCounts[status])
	}
	aggregate.rejectionRate /= float64(aggregate.total)
	aggregate.errorRate = float64(aggregate.errors) / float64(aggregate.total)
	aggregate.p95 = r09Percentile(aggregate.latencies, 0.95)
	aggregate.p99 = r09Percentile(aggregate.latencies, 0.99)
	return aggregate
}

func logR09Window(t *testing.T, variant string, window r09Window) {
	t.Helper()
	first := window.pool.instances["server-1"]
	second := window.pool.instances["server-2"]
	t.Logf("R09 %s %s: total=%d success=%d goodput=%.1f/s p95=%s p99=%s rejection=%.2f%% errors=%.2f%% db_questions=%d db_select=%d rows_read=%d rows_sent=%d bytes_sent=%d db_ops=%d db_time_ms=%.3f cache_hit=%d cache_miss=%d rate_allow=%d rate_deny=%d resource_reject=%d redis_eval=%d/%dus redis_evalsha=%d/%dus redis_get=%d/%dus redis_set=%d/%dus redis_del=%d/%dus pool_wait_count=%d pool_wait_duration=%s server1_pool=%d/%d/%d/%d wait=%d/%s server2_pool=%d/%d/%d/%d wait=%d/%s", variant, window.name, window.total, window.success, window.goodput, window.p95, window.p99, window.rejectionRate*100, window.errorRate*100, window.db.Questions, window.db.ComSelect, window.db.RowsRead, window.db.RowsSent, window.db.BytesSent, window.metrics.dbOperations, window.metrics.dbOperationSeconds*1000, window.metrics.cacheHits, window.metrics.cacheMisses, window.metrics.rateLimitAllows, window.metrics.rateLimitDenies, window.metrics.resourceRejects, window.redis.evalCalls, window.redis.evalUsec, window.redis.evalSHACalls, window.redis.evalSHAUsec, window.redis.getCalls, window.redis.getUsec, window.redis.setCalls, window.redis.setUsec, window.redis.delCalls, window.redis.delUsec, window.pool.waitCount, window.pool.waitDuration, first.maxOpen, first.open, first.inUse, first.idle, first.waitCount, first.waitDuration, second.maxOpen, second.open, second.inUse, second.idle, second.waitCount, second.waitDuration)
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

func runR09Window(t *testing.T, name string, pattern r09AccessPattern, clients []*http.Client, server1, server2 *protectionInstance, postIDs []uint64, requestsPerWorker int) r09Window {
	t.Helper()
	if len(clients) == 0 || len(postIDs) == 0 || requestsPerWorker <= 0 {
		t.Fatalf("invalid R09 window clients=%d posts=%d requests_per_worker=%d", len(clients), len(postIDs), requestsPerWorker)
	}
	poolBefore := captureR09PoolSnapshot(server1, server2)
	results := make(chan r09RequestResult, len(clients)*requestsPerWorker)
	start := make(chan struct{})
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
				postID := r09PostID(pattern, postIDs, worker, requestIndex)
				status, elapsed, err := r09Read(client, address, postID)
				results <- r09RequestResult{status: status, elapsed: elapsed, err: err}
			}
		}(worker, client)
	}
	started := time.Now()
	close(start)
	group.Wait()
	close(results)

	window := r09Window{name: name, pattern: pattern, total: len(clients) * requestsPerWorker, statusCounts: make(map[int]int)}
	for result := range results {
		window.statusCounts[result.status]++
		if result.err != nil {
			window.errors++
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
	window.errorRate = float64(window.errors) / float64(window.total)
	window.p95 = r09Percentile(window.latencies, 0.95)
	window.p99 = r09Percentile(window.latencies, 0.99)
	window.pool = poolBefore.delta(captureR09PoolSnapshot(server1, server2))
	return window
}

func r09PostID(pattern r09AccessPattern, postIDs []uint64, worker, requestIndex int) uint64 {
	if pattern == r09UniformPattern && len(postIDs) > 1 {
		return postIDs[1+(worker+requestIndex)%min(r09UniformPostCount, len(postIDs)-1)]
	}
	return postIDs[0]
}

func captureR09PoolSnapshot(instances ...*protectionInstance) r09PoolSnapshot {
	snapshot := r09PoolSnapshot{instances: make(map[string]r09PoolInstanceEvidence, len(instances))}
	for _, instance := range instances {
		if instance == nil || instance.assembly == nil || instance.assembly.database == nil {
			continue
		}
		stats := instance.assembly.database.Stats()
		snapshot.instances[instance.name] = r09PoolInstanceEvidence{
			maxOpen:      stats.MaxOpenConnections,
			open:         stats.OpenConnections,
			inUse:        stats.InUse,
			idle:         stats.Idle,
			waitCount:    stats.WaitCount,
			waitDuration: stats.WaitDuration,
		}
	}
	return snapshot
}

func (before r09PoolSnapshot) delta(after r09PoolSnapshot) r09PoolEvidence {
	evidence := r09PoolEvidence{instances: make(map[string]r09PoolInstanceEvidence, len(after.instances))}
	for name, current := range after.instances {
		previous := before.instances[name]
		value := r09PoolInstanceEvidence{
			maxOpen:      current.maxOpen,
			open:         current.open,
			inUse:        current.inUse,
			idle:         current.idle,
			waitCount:    current.waitCount - previous.waitCount,
			waitDuration: current.waitDuration - previous.waitDuration,
		}
		if value.waitCount < 0 {
			value.waitCount = 0
		}
		if value.waitDuration < 0 {
			value.waitDuration = 0
		}
		evidence.instances[name] = value
		evidence.waitCount += value.waitCount
		evidence.waitDuration += value.waitDuration
	}
	return evidence
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

func r09MedianGoodput(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
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

func r09ReadRedisStatus(t *testing.T, cfg config.Config) r09RedisStatus {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{
		Addr:         net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     1,
		MaxRetries:   0,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := client.Info(ctx, "commandstats").Result()
	if err != nil {
		t.Fatalf("read R09 Redis commandstats: %v", err)
	}
	status := r09RedisStatus{commands: make(map[string]r09RedisCommandEvidence)}
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "cmdstat_") {
			continue
		}
		nameAndFields := strings.SplitN(line, ":", 2)
		if len(nameAndFields) != 2 {
			continue
		}
		name := strings.TrimPrefix(nameAndFields[0], "cmdstat_")
		evidence := r09RedisCommandEvidence{}
		for _, field := range strings.Split(nameAndFields[1], ",") {
			key, value, found := strings.Cut(field, "=")
			if !found {
				continue
			}
			parsed, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil {
				continue
			}
			switch key {
			case "calls":
				evidence.calls = parsed
			case "usec":
				evidence.usec = parsed
			}
		}
		status.commands[name] = evidence
	}
	return status
}

func (after r09RedisStatus) delta(before r09RedisStatus) r09RedisDelta {
	commandDelta := func(name string) r09RedisCommandEvidence {
		current := after.commands[name]
		previous := before.commands[name]
		current.calls -= previous.calls
		current.usec -= previous.usec
		if current.calls < 0 {
			current.calls = 0
		}
		if current.usec < 0 {
			current.usec = 0
		}
		return current
	}
	eval := commandDelta("eval")
	evalSHA := commandDelta("evalsha")
	get := commandDelta("get")
	set := commandDelta("set")
	del := commandDelta("del")
	return r09RedisDelta{
		evalCalls:    eval.calls,
		evalUsec:     eval.usec,
		evalSHACalls: evalSHA.calls,
		evalSHAUsec:  evalSHA.usec,
		getCalls:     get.calls,
		getUsec:      get.usec,
		setCalls:     set.calls,
		setUsec:      set.usec,
		delCalls:     del.calls,
		delUsec:      del.usec,
	}
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

func resetR09RateLimitBuckets(t *testing.T, cfg config.Config, userIDs []uint64) {
	t.Helper()
	redisClient := platform.NewRedis(cfg.Redis)
	defer redisClient.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, userID := range userIDs {
		key := "gopulse:rl:v1:post_read:user:" + strconv.FormatUint(userID, 10)
		if err := redisClient.Delete(ctx, key); err != nil {
			t.Fatalf("reset R09 rate limit bucket %s: %v", key, err)
		}
	}
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

func r09ReadBusinessBudget(t *testing.T, database *sql.DB) r09BusinessBudget {
	t.Helper()
	var budget r09BusinessBudget
	err := database.QueryRowContext(context.Background(), `
		SELECT e.data_epoch, b.open_requirements, b.retained_groups, b.retained_bytes, b.parked_messages, b.parked_bytes
		FROM business_environment AS e
		INNER JOIN business_async_budget AS b ON b.data_epoch=e.data_epoch
		WHERE e.singleton=1`).Scan(&budget.epoch, &budget.open, &budget.retainedGroups, &budget.retainedBytes, &budget.parkedMessages, &budget.parkedBytes)
	if err != nil {
		t.Fatalf("read R09 business budget: %v", err)
	}
	return budget
}

func r09SQLPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func cleanupR09Fixture(t *testing.T, database *sql.DB, userIDs []uint64, postIDs []uint64, budget r09BusinessBudget) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	postArguments := make([]any, len(postIDs))
	for index, postID := range postIDs {
		postArguments[index] = postID
	}
	postPlaceholders := r09SQLPlaceholders(len(postArguments))
	eventArgumentsByPost := append(append([]any(nil), postArguments...), postArguments...)
	rows, err := database.QueryContext(ctx, `
		SELECT event_id FROM business_outbox
		WHERE CAST(JSON_UNQUOTE(JSON_EXTRACT(payload, '$.post_id')) AS UNSIGNED) IN (`+postPlaceholders+`)
		UNION
		SELECT event_id FROM business_commands
		WHERE post_id IN (`+postPlaceholders+`) AND event_id IS NOT NULL`, eventArgumentsByPost...)
	if err != nil {
		t.Errorf("read R09 outbox event IDs: %v", err)
	} else {
		eventIDs := make([]string, 0, len(postIDs))
		for rows.Next() {
			var eventID string
			if scanErr := rows.Scan(&eventID); scanErr != nil {
				t.Errorf("scan R09 outbox event ID: %v", scanErr)
				break
			}
			eventIDs = append(eventIDs, eventID)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			t.Errorf("iterate R09 outbox event IDs: %v", rowsErr)
		}
		if closeErr := rows.Close(); closeErr != nil {
			t.Errorf("close R09 outbox event IDs: %v", closeErr)
		}
		if len(eventIDs) > 0 {
			eventArguments := make([]any, len(eventIDs))
			for index, eventID := range eventIDs {
				eventArguments[index] = eventID
			}
			eventPlaceholders := r09SQLPlaceholders(len(eventArguments))
			for _, statement := range []string{
				`DELETE FROM business_application_results WHERE event_id IN (` + eventPlaceholders + `)`,
				`DELETE FROM business_event_failures WHERE event_id IN (` + eventPlaceholders + `)`,
				`DELETE FROM business_message_parking WHERE event_id IN (` + eventPlaceholders + `)`,
				`DELETE FROM business_event_requirements WHERE event_id IN (` + eventPlaceholders + `)`,
				`DELETE FROM business_outbox WHERE event_id IN (` + eventPlaceholders + `)`,
			} {
				if _, execErr := database.ExecContext(ctx, statement, eventArguments...); execErr != nil {
					t.Errorf("cleanup R09 event evidence: %v", execErr)
				}
			}
		}
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM business_commands WHERE post_id IN (`+postPlaceholders+`)`, postArguments...); err != nil {
		t.Errorf("cleanup R09 business commands: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE notifications SET post_id=NULL, comment_id=NULL WHERE post_id IN (`+postPlaceholders+`)`, postArguments...); err != nil {
		t.Errorf("cleanup R09 notifications: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE business_async_budget SET open_requirements=?, retained_groups=?, retained_bytes=?, parked_messages=?, parked_bytes=? WHERE data_epoch=?`, budget.open, budget.retainedGroups, budget.retainedBytes, budget.parkedMessages, budget.parkedBytes, budget.epoch); err != nil {
		t.Errorf("restore R09 business budget: %v", err)
	}
	for _, postID := range postIDs {
		for _, statement := range []string{
			"DELETE FROM post_bookmarks WHERE post_id=?",
			"DELETE FROM post_likes WHERE post_id=?",
			"DELETE FROM comments WHERE post_id=?",
			"DELETE FROM posts WHERE id=?",
		} {
			if _, err := database.ExecContext(ctx, statement, postID); err != nil {
				t.Errorf("cleanup R09 fixture post=%d: %v", postID, err)
			}
		}
	}
	for _, userID := range userIDs {
		if _, err := database.ExecContext(ctx, "DELETE FROM users WHERE id=?", userID); err != nil {
			t.Errorf("cleanup R09 user %d: %v", userID, err)
		}
	}
	var remainingPosts int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE id IN (`+postPlaceholders+`)`, postArguments...).Scan(&remainingPosts); err != nil {
		t.Errorf("verify R09 posts cleanup: %v", err)
	} else if remainingPosts != 0 {
		t.Errorf("verify R09 posts cleanup: %d fixture posts remain", remainingPosts)
	}
	userArguments := make([]any, len(userIDs))
	for index, userID := range userIDs {
		userArguments[index] = userID
	}
	userPlaceholders := r09SQLPlaceholders(len(userArguments))
	var remainingUsers int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id IN (`+userPlaceholders+`)`, userArguments...).Scan(&remainingUsers); err != nil {
		t.Errorf("verify R09 users cleanup: %v", err)
	} else if remainingUsers != 0 {
		t.Errorf("verify R09 users cleanup: %d fixture users remain", remainingUsers)
	}
}

func assertR09Contract(t *testing.T, results map[string]r09VariantResult) {
	t.Helper()
	baseline, ok := results["A_body"]
	if !ok || baseline.warmHotspot.success == 0 || baseline.warmHotspot.goodput <= 0 || baseline.warmUniform.success == 0 || baseline.warmUniform.goodput <= 0 {
		t.Fatalf("R09 body path has no usable reference: %#v", baseline)
	}
	result, ok := results["B_protected_body"]
	if !ok {
		t.Fatal("R09 protected body result is missing")
	}
	name := "B_protected_body"
	assertR09ColdWindow(t, name, "hotspot", result.coldHotspot)
	assertR09ColdWindow(t, name, "uniform", result.coldUniform)
	assertR09NormalWindow(t, name, "hotspot", result.warmHotspot, baseline.warmHotspot, r09ProtectedGoodputMin)
	assertR09NormalWindow(t, name, "uniform", result.warmUniform, baseline.warmUniform, r09ProtectedGoodputMin)
	assertR09FaultWindow(t, name, "hotspot", result.faultHotspot)
	assertR09FaultWindow(t, name, "uniform", result.faultUniform)
	assertR09RecoveryWindow(t, name, "hotspot", result.recoveryHotspot)
	assertR09RecoveryWindow(t, name, "uniform", result.recoveryUniform)
	for _, window := range []r09Window{baseline.warmHotspot, baseline.warmUniform, result.warmHotspot, result.warmUniform} {
		work := float64(window.db.Questions) / float64(window.success)
		// Historical v1 warm reads used 6.002 Questions/success. Preserve its 5% reduction budget.
		// Evidence: R09 warm matrix in commit 312a6fb (historical Exporter acceptance report).
		if work > 6.002*r09DBWorkMaxRatio {
			t.Errorf("R09 %s %s database work %.3f exceeds historical budget %.3f", window.name, window.pattern, work, 6.002*r09DBWorkMaxRatio)
		}
	}
}

func assertR09ColdWindow(t *testing.T, variant, pattern string, result r09Window) {
	t.Helper()
	if result.success != result.total || result.errorRate > 0 || result.rejectionRate > r09MaxNormalRejectRate {
		t.Errorf("R09 %s %s cold success=%d/%d errors=%.2f%% rejection=%.2f%% statuses=%v", variant, pattern, result.success, result.total, result.errorRate*100, result.rejectionRate*100, result.statusCounts)
	}
	if result.p95 > r09P95Limit || result.p99 > r09P99Limit {
		t.Errorf("R09 %s %s cold tail latency p95=%s p99=%s exceeds %s/%s", variant, pattern, result.p95, result.p99, r09P95Limit, r09P99Limit)
	}
	assertR09PoolEvidence(t, variant+"/cold_"+pattern, result)
}

func assertR09NormalWindow(t *testing.T, variant, pattern string, result, baseline r09Window, goodputFloor float64) {
	t.Helper()
	if result.errorRate > 0 {
		t.Errorf("R09 %s %s has %d request errors", variant, pattern, result.errors)
	}
	if result.p95 > r09P95Limit || result.p99 > r09P99Limit {
		t.Errorf("R09 %s %s tail latency p95=%s p99=%s exceeds %s/%s", variant, pattern, result.p95, result.p99, r09P95Limit, r09P99Limit)
	}
	if result.goodput < baseline.goodput*goodputFloor {
		t.Errorf("R09 %s %s goodput %.1f/s is below %.0f%% of reference %.1f/s", variant, pattern, result.goodput, goodputFloor*100, baseline.goodput)
	}
	if result.rejectionRate > r09MaxNormalRejectRate {
		t.Errorf("R09 %s %s rejection rate %.2f%% exceeds %.2f%%", variant, pattern, result.rejectionRate*100, r09MaxNormalRejectRate*100)
	}
	assertR09PoolEvidence(t, variant+"/"+pattern, result)
}

func assertR09FaultWindow(t *testing.T, variant, pattern string, result r09Window) {
	t.Helper()
	if result.success != result.total || result.errorRate > 0 || result.rejectionRate > r09MaxNormalRejectRate {
		t.Errorf("R09 %s Redis fault %s success=%d/%d errors=%.2f%% rejection=%.2f%% statuses=%v", variant, pattern, result.success, result.total, result.errorRate*100, result.rejectionRate*100, result.statusCounts)
	}
	if result.p95 > r09FaultP95Limit || result.p99 > r09FaultP99Limit {
		t.Errorf("R09 %s Redis fault %s tail latency p95=%s p99=%s exceeds %s/%s", variant, pattern, result.p95, result.p99, r09FaultP95Limit, r09FaultP99Limit)
	}
	assertR09PoolEvidence(t, variant+"/fault_"+pattern, result)
}

func assertR09RecoveryWindow(t *testing.T, variant, pattern string, result r09Window) {
	t.Helper()
	if result.success != result.total || result.errorRate > 0 || result.rejectionRate > r09MaxNormalRejectRate {
		t.Errorf("R09 %s Redis recovery %s success=%d/%d errors=%.2f%% rejection=%.2f%% statuses=%v", variant, pattern, result.success, result.total, result.errorRate*100, result.rejectionRate*100, result.statusCounts)
	}
	if result.p95 > r09P95Limit || result.p99 > r09P99Limit {
		t.Errorf("R09 %s Redis recovery %s tail latency p95=%s p99=%s exceeds %s/%s", variant, pattern, result.p95, result.p99, r09P95Limit, r09P99Limit)
	}
	assertR09PoolEvidence(t, variant+"/recovery_"+pattern, result)
}

func assertR09PoolEvidence(t *testing.T, name string, result r09Window) {
	t.Helper()
	if result.success == 0 {
		return
	}
	waitPerSuccess := result.pool.waitDuration / time.Duration(result.success)
	if waitPerSuccess > r09PoolWaitDurationPerSuccessLimit {
		t.Errorf("R09 %s pool wait duration per successful request=%s exceeds %s", name, waitPerSuccess, r09PoolWaitDurationPerSuccessLimit)
	}
	if result.pool.waitCount > 0 {
		averageWait := result.pool.waitDuration / time.Duration(result.pool.waitCount)
		if averageWait > r09PoolWaitAvgLimit {
			t.Errorf("R09 %s pool average wait=%s exceeds %s", name, averageWait, r09PoolWaitAvgLimit)
		}
	}
	for instance, stats := range result.pool.instances {
		if stats.maxOpen != r09ExpectedPoolMaxOpen {
			t.Errorf("R09 %s %s max open connections=%d want %d", name, instance, stats.maxOpen, r09ExpectedPoolMaxOpen)
		}
	}
}
