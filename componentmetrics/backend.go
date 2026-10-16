package componentmetrics

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Route contains only a server-registered method and template, never a URL.
type Route struct{ Method, Template string }

type httpTuple struct {
	route Route
	class int
}
type completedRequest struct {
	count   uint64
	seconds float64
}

type durationCounter struct {
	count atomic.Uint64
	nanos atomic.Uint64
}
type requestState struct {
	count   uint64
	seconds float64
	buckets [backendLatencyBucketSlots]uint64
}
type requestSeries struct {
	labels string
	value  atomic.Pointer[requestState]
}
type backlog struct {
	pending       int64
	oldestSeconds float64
	valid         bool
}

// Backend stores a fixed set of series slots allocated at router construction.
// Hot-path updates perform no I/O, acquire no mutex, and publish one immutable
// fixed-size state so the legacy pair and histogram can never be scraped from
// different completions.
type Backend struct {
	requests             map[httpTuple]*requestSeries
	ordered              []*requestSeries
	dependencies         [4]atomic.Int32
	outbox               atomic.Pointer[backlog]
	lastPublish          atomic.Int64
	httpInFlight         atomic.Int64
	httpConcurrencyLimit atomic.Int64
	httpRejected         atomic.Uint64
	alertKnown           [3]atomic.Int32
	alertLastSuccess     [3]atomic.Int64
	freshness            [10]atomic.Pointer[completedRequest]
	traceContextInvalid  atomic.Uint64
	cacheRequests        [4]atomic.Uint64
	cacheOrigins         [3]durationCounter
	rateLimitDecisions   [5][2][3]atomic.Uint64
	resourceRejected     [3][3]atomic.Uint64
	dbOperations         [4][3]durationCounter
	rebuildInFlight      atomic.Int64
	rebuildWaiters       atomic.Int64
	rebuildRegistered    atomic.Int64
}

var backendMethods = [...]string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE", "unknown"}
var backendDependencies = [...]string{"mysql", "redis", "rabbitmq", "elasticsearch"}

const BackendFamilies = 28

// BackendMaxRoutes guards accidental unbounded registration, not client input.
const BackendMaxRoutes = 64
const BackendMaxBodyBytes = 1 << 20

const backendLatencyBucketSlots = 12

var backendFreshnessStages = [...]string{"commit", "publish", "consume", "index", "visible"}

var backendLatencyBucketValues = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10}
var backendLatencyBucketLabels = [...]string{"0.005", "0.01", "0.025", "0.05", "0.1", "0.25", "0.5", "1", "2", "5", "10", "+Inf"}
var backendCacheResults = [...]string{"hit", "miss", "invalid", "error"}
var backendCacheOrigins = [...]string{"success", "failure", "version_changed"}
var backendRatePolicies = [...]string{"auth_source", "login_attempt", "post_read", "fact_write", "interaction_like"}
var backendProtectionResults = [...]string{"allow", "deny", "error"}
var backendResourceKinds = [...]string{"auth", "read", "write"}
var backendDBOperations = [...]string{"post_context", "post_body", "post_dynamic", "post_viewer"}
var backendDBResults = [...]string{"success", "failure", "version_changed"}

// BackendLatencyBuckets returns the fixed label vocabulary for the Backend
// latency distribution. The copy prevents callers from widening the contract.
func BackendLatencyBuckets() []string {
	return append([]string(nil), backendLatencyBucketLabels[:]...)
}

// NewBackend freezes exactly the supplied registered method/template pairs plus
// a fixed unmatched bucket per allowed method. Counter tuples remain absent
// until first completion; gauges have explicit zero/unknown initial semantics.
func NewBackend(routes []Route) (*Backend, error) {
	if len(routes) > BackendMaxRoutes {
		return nil, errors.New("too many backend metric routes")
	}
	b := &Backend{requests: make(map[httpTuple]*requestSeries)}
	for i := range b.dependencies {
		b.dependencies[i].Store(-1)
	}
	b.outbox.Store(&backlog{})
	seen := make(map[Route]bool)
	for _, route := range routes {
		if !knownMethod(route.Method) || route.Method == "unknown" || !strings.HasPrefix(route.Template, "/") || len(route.Template) > 160 || strings.ContainsAny(route.Template, "\"\\\r\n?{}") {
			return nil, errors.New("invalid backend metric route")
		}
		if seen[route] {
			continue
		}
		seen[route] = true
		b.addRoute(route)
	}
	for _, method := range backendMethods {
		b.addRoute(Route{Method: method, Template: "_unmatched"})
	}
	return b, nil
}

func knownMethod(method string) bool {
	for _, allowed := range backendMethods {
		if method == allowed {
			return true
		}
	}
	return false
}
func (b *Backend) addRoute(route Route) {
	for class := 1; class <= 5; class++ {
		series := &requestSeries{labels: fmt.Sprintf("{method=%q,route=%q,status_class=%q}", route.Method, route.Template, strconv.Itoa(class)+"xx")}
		b.requests[httpTuple{route: route, class: class}] = series
		b.ordered = append(b.ordered, series)
	}
}

func (b *Backend) ObserveRequest(method, template string, status int, elapsed time.Duration) {
	if b == nil || elapsed < 0 || status < 100 || status >= 600 {
		return
	}
	if !knownMethod(method) {
		method = "unknown"
	}
	tuple := httpTuple{route: Route{Method: method, Template: template}, class: status / 100}
	series := b.requests[tuple]
	if series == nil {
		tuple.route.Template = "_unmatched"
		series = b.requests[tuple]
	}
	next := &requestState{}
	for {
		old := series.value.Load()
		*next = requestState{count: 1, seconds: elapsed.Seconds()}
		if old != nil {
			next.count += old.count
			next.seconds += old.seconds
			next.buckets = old.buckets
		}
		seconds := elapsed.Seconds()
		for i, bound := range backendLatencyBucketValues {
			if seconds <= bound {
				next.buckets[i]++
			}
		}
		next.buckets[len(next.buckets)-1]++
		if series.value.CompareAndSwap(old, next) {
			return
		}
	}
}

func (b *Backend) SetHTTPConcurrencyLimit(limit int) {
	if b == nil || limit < 0 {
		return
	}
	b.httpConcurrencyLimit.Store(int64(limit))
}

func (b *Backend) ObserveHTTPStarted() {
	if b != nil {
		b.httpInFlight.Add(1)
	}
}

func (b *Backend) ObserveHTTPFinished() {
	if b == nil {
		return
	}
	for {
		current := b.httpInFlight.Load()
		if current <= 0 || b.httpInFlight.CompareAndSwap(current, current-1) {
			return
		}
	}
}

func (b *Backend) ObserveHTTPRejected() {
	if b != nil {
		b.httpRejected.Add(1)
	}
}

func (b *Backend) ObserveAlert(suffix string, value float64, source string) {
	if b == nil || (suffix != "alert_evaluation_known" && suffix != "alert_last_success_timestamp_seconds") {
		return
	}
	index := -1
	for i, allowed := range [...]string{"metrics", "logs", "events"} {
		if source == allowed {
			index = i
			break
		}
	}
	if index < 0 || value < 0 || value != float64(int64(value)) {
		return
	}
	if suffix == "alert_evaluation_known" {
		if value != 0 && value != 1 {
			return
		}
		b.alertKnown[index].Store(int32(value))
		return
	}
	b.alertLastSuccess[index].Store(int64(value))
}

func (b *Backend) ObserveDependency(name string, err error) {
	if b == nil {
		return
	}
	for i, allowed := range backendDependencies {
		if name == allowed {
			var value int32 = 1
			if err != nil {
				value = 0
			}
			b.dependencies[i].Store(value)
			return
		}
	}
}

// ObserveOutbox never turns a failed/unknown snapshot into an empty queue.
func (b *Backend) ObserveOutbox(pending int64, oldest time.Duration, err error) {
	if b == nil {
		return
	}
	b.ObserveDependency("mysql", err)
	if err != nil || pending < 0 || oldest < 0 {
		b.outbox.Store(&backlog{})
		return
	}
	b.outbox.Store(&backlog{pending: pending, oldestSeconds: oldest.Seconds(), valid: true})
}
func (b *Backend) ObservePublish(at time.Time, err error) {
	if b == nil {
		return
	}
	b.ObserveDependency("rabbitmq", err)
	if err == nil {
		b.lastPublish.Store(at.Unix())
	}
}

// MaxSamples includes the historical counter pair, all fixed distribution
// members, capacity signals, dependencies, and outbox gauges. No client value
// can increase it.
func (b *Backend) MaxSamples() int { return len(b.ordered)*16 + 113 }

func (b *Backend) ObserveFreshness(stage, result string, elapsed time.Duration) {
	if b == nil || elapsed < 0 || (result != "success" && result != "failure") {
		return
	}
	stageIndex := -1
	for index, allowed := range backendFreshnessStages {
		if stage == allowed {
			stageIndex = index
			break
		}
	}
	if stageIndex < 0 {
		return
	}
	if result == "failure" {
		stageIndex += len(backendFreshnessStages)
	}
	next := &completedRequest{count: 1, seconds: elapsed.Seconds()}
	cell := &b.freshness[stageIndex]
	for {
		old := cell.Load()
		if old != nil {
			next.count += old.count
			next.seconds += old.seconds
		}
		if cell.CompareAndSwap(old, next) {
			return
		}
		next = &completedRequest{count: 1, seconds: elapsed.Seconds()}
	}
}

func (b *Backend) ObserveTraceContextInvalid() {
	if b != nil {
		b.traceContextInvalid.Add(1)
	}
}

func (b *Backend) ObserveCacheRequest(result string) {
	if b == nil {
		return
	}
	for index, allowed := range backendCacheResults {
		if result == allowed {
			b.cacheRequests[index].Add(1)
			return
		}
	}
}

func (b *Backend) ObserveCacheOrigin(result string) {
	b.observeCacheOrigin(result, 0)
}

func (b *Backend) ObserveCacheOriginDuration(result string, elapsed time.Duration) {
	b.observeCacheOrigin(result, elapsed)
}

func (b *Backend) observeCacheOrigin(result string, elapsed time.Duration) {
	if b == nil || elapsed < 0 {
		return
	}
	for index, allowed := range backendCacheOrigins {
		if result == allowed {
			b.cacheOrigins[index].count.Add(1)
			b.cacheOrigins[index].nanos.Add(uint64(elapsed))
			return
		}
	}
}

func (b *Backend) ObserveRateLimit(policy, mode, result string) {
	if b == nil {
		return
	}
	policyIndex, resultIndex := -1, -1
	for index, allowed := range backendRatePolicies {
		if policy == allowed {
			policyIndex = index
			break
		}
	}
	for index, allowed := range backendProtectionResults {
		if result == allowed {
			resultIndex = index
			break
		}
	}
	if policyIndex < 0 || resultIndex < 0 || (mode != "shared" && mode != "local") {
		return
	}
	modeIndex := 0
	if mode == "local" {
		modeIndex = 1
	}
	b.rateLimitDecisions[policyIndex][modeIndex][resultIndex].Add(1)
}

func (b *Backend) ObserveResourceRejected(kind, reason string) {
	if b == nil {
		return
	}
	kindIndex, reasonIndex := -1, -1
	for index, allowed := range backendResourceKinds {
		if kind == allowed {
			kindIndex = index
			break
		}
	}
	for index, allowed := range [...]string{"capacity", "rate_limited", "unavailable"} {
		if reason == allowed {
			reasonIndex = index
			break
		}
	}
	if kindIndex >= 0 && reasonIndex >= 0 {
		b.resourceRejected[kindIndex][reasonIndex].Add(1)
	}
}

func (b *Backend) ObserveDBOperation(operation, result string, elapsed time.Duration) {
	if b == nil || elapsed < 0 {
		return
	}
	operationIndex, resultIndex := -1, -1
	for index, allowed := range backendDBOperations {
		if operation == allowed {
			operationIndex = index
			break
		}
	}
	for index, allowed := range backendDBResults {
		if result == allowed {
			resultIndex = index
			break
		}
	}
	if operationIndex < 0 || resultIndex < 0 {
		return
	}
	cell := &b.dbOperations[operationIndex][resultIndex]
	cell.count.Add(1)
	cell.nanos.Add(uint64(elapsed))
}

func (b *Backend) ObserveRebuildState(active, waiters, registered int) {
	if b == nil || active < 0 || waiters < 0 || registered < 0 {
		return
	}
	b.rebuildInFlight.Store(int64(active))
	b.rebuildWaiters.Store(int64(waiters))
	b.rebuildRegistered.Store(int64(registered))
}

func (b *Backend) Snapshot() ([]byte, bool) {
	state := b.outbox.Load()
	if state == nil || !state.valid {
		return nil, false
	}
	var out strings.Builder
	out.WriteString("# TYPE gopulse_backend_http_requests_total counter\n# TYPE gopulse_backend_http_request_duration_seconds_total counter\n# TYPE gopulse_backend_http_request_duration_seconds_bucket counter\n# TYPE gopulse_backend_http_request_duration_seconds_count counter\n# TYPE gopulse_backend_http_request_duration_seconds_sum counter\n")
	for _, series := range b.ordered {
		value := series.value.Load()
		if value == nil {
			continue
		}
		fmt.Fprintf(&out, "gopulse_backend_http_requests_total%s %d\ngopulse_backend_http_request_duration_seconds_total%s %s\n", series.labels, value.count, series.labels, strconv.FormatFloat(value.seconds, 'g', -1, 64))
		for i, bucket := range backendLatencyBucketLabels {
			fmt.Fprintf(&out, "gopulse_backend_http_request_duration_seconds_bucket%s %d\n", withLabel(series.labels, "le", bucket), value.buckets[i])
		}
		fmt.Fprintf(&out, "gopulse_backend_http_request_duration_seconds_count%s %d\ngopulse_backend_http_request_duration_seconds_sum%s %s\n", series.labels, value.count, series.labels, strconv.FormatFloat(value.seconds, 'g', -1, 64))
	}
	fmt.Fprintf(&out, "# TYPE gopulse_backend_alert_evaluation_known gauge\n# TYPE gopulse_backend_alert_last_success_timestamp_seconds gauge\n")
	for i, source := range [...]string{"metrics", "logs", "events"} {
		fmt.Fprintf(&out, "gopulse_backend_alert_evaluation_known{alert_source=%q} %d\ngopulse_backend_alert_last_success_timestamp_seconds{alert_source=%q} %d\n", source, b.alertKnown[i].Load(), source, b.alertLastSuccess[i].Load())
	}
	fmt.Fprintf(&out, "# TYPE gopulse_backend_outbox_pending gauge\ngopulse_backend_outbox_pending %d\n# TYPE gopulse_backend_outbox_oldest_age_seconds gauge\ngopulse_backend_outbox_oldest_age_seconds %s\n# TYPE gopulse_backend_outbox_last_publish_success_timestamp_seconds gauge\ngopulse_backend_outbox_last_publish_success_timestamp_seconds %d\n# TYPE gopulse_backend_http_requests_in_flight gauge\ngopulse_backend_http_requests_in_flight %d\n# TYPE gopulse_backend_http_concurrency_limit gauge\ngopulse_backend_http_concurrency_limit %d\n# TYPE gopulse_backend_http_rejected_total counter\ngopulse_backend_http_rejected_total %d\n# TYPE gopulse_backend_trace_context_invalid_total counter\ngopulse_backend_trace_context_invalid_total %d\n", state.pending, strconv.FormatFloat(state.oldestSeconds, 'g', -1, 64), b.lastPublish.Load(), b.httpInFlight.Load(), b.httpConcurrencyLimit.Load(), b.httpRejected.Load(), b.traceContextInvalid.Load())
	fmt.Fprintf(&out, "# TYPE gopulse_backend_freshness_events_total counter\n# TYPE gopulse_backend_freshness_duration_seconds_total counter\n")
	for index, stage := range backendFreshnessStages {
		for resultIndex, result := range [...]string{"success", "failure"} {
			cell := b.freshness[index+resultIndex*len(backendFreshnessStages)].Load()
			if cell == nil {
				continue
			}
			labels := fmt.Sprintf("{stage=%q,result=%q}", stage, result)
			fmt.Fprintf(&out, "gopulse_backend_freshness_events_total%s %d\ngopulse_backend_freshness_duration_seconds_total%s %s\n", labels, cell.count, labels, strconv.FormatFloat(cell.seconds, 'g', -1, 64))
		}
	}
	out.WriteString("# TYPE gopulse_backend_dependency_up gauge\n")
	for i, name := range backendDependencies {
		fmt.Fprintf(&out, "gopulse_backend_dependency_up{dependency=%q} %d\n", name, b.dependencies[i].Load())
	}
	out.WriteString("# TYPE gopulse_backend_cache_requests_total counter\n")
	for index, result := range backendCacheResults {
		if value := b.cacheRequests[index].Load(); value > 0 {
			fmt.Fprintf(&out, "gopulse_backend_cache_requests_total{result=%q} %d\n", result, value)
		}
	}
	out.WriteString("# TYPE gopulse_backend_cache_origin_total counter\n")
	for index, result := range backendCacheOrigins {
		if value := b.cacheOrigins[index].count.Load(); value > 0 {
			labels := fmt.Sprintf("{result=%q}", result)
			fmt.Fprintf(&out, "gopulse_backend_cache_origin_total%s %d\ngopulse_backend_cache_origin_duration_seconds_total%s %s\n", labels, value, labels, strconv.FormatFloat(float64(b.cacheOrigins[index].nanos.Load())/float64(time.Second), 'g', -1, 64))
		}
	}
	out.WriteString("# TYPE gopulse_backend_rate_limit_decisions_total counter\n")
	for policyIndex, policy := range backendRatePolicies {
		for modeIndex, mode := range [...]string{"shared", "local"} {
			for resultIndex, result := range backendProtectionResults {
				if value := b.rateLimitDecisions[policyIndex][modeIndex][resultIndex].Load(); value > 0 {
					fmt.Fprintf(&out, "gopulse_backend_rate_limit_decisions_total{policy=%q,mode=%q,result=%q} %d\n", policy, mode, result, value)
				}
			}
		}
	}
	out.WriteString("# TYPE gopulse_backend_resource_rejected_total counter\n")
	for kindIndex, kind := range backendResourceKinds {
		for reasonIndex, reason := range [...]string{"capacity", "rate_limited", "unavailable"} {
			if value := b.resourceRejected[kindIndex][reasonIndex].Load(); value > 0 {
				fmt.Fprintf(&out, "gopulse_backend_resource_rejected_total{resource=%q,reason=%q} %d\n", kind, reason, value)
			}
		}
	}
	out.WriteString("# TYPE gopulse_backend_db_operations_total counter\n# TYPE gopulse_backend_db_operation_duration_seconds_total counter\n")
	for operationIndex, operation := range backendDBOperations {
		for resultIndex, result := range backendDBResults {
			cell := &b.dbOperations[operationIndex][resultIndex]
			if value := cell.count.Load(); value > 0 {
				fmt.Fprintf(&out, "gopulse_backend_db_operations_total{operation=%q,result=%q} %d\n", operation, result, value)
				fmt.Fprintf(&out, "gopulse_backend_db_operation_duration_seconds_total{operation=%q,result=%q} %s\n", operation, result, strconv.FormatFloat(float64(cell.nanos.Load())/float64(time.Second), 'g', -1, 64))
			}
		}
	}
	fmt.Fprintf(&out, "# TYPE gopulse_backend_cache_rebuild_in_flight gauge\ngopulse_backend_cache_rebuild_in_flight %d\n# TYPE gopulse_backend_cache_waiters gauge\ngopulse_backend_cache_waiters %d\n# TYPE gopulse_backend_cache_registered_waiters gauge\ngopulse_backend_cache_registered_waiters %d\n", b.rebuildInFlight.Load(), b.rebuildWaiters.Load(), b.rebuildRegistered.Load())
	if out.Len() > BackendMaxBodyBytes {
		return nil, false
	}
	return []byte(out.String()), true
}

func withLabel(labels, key, value string) string {
	return strings.TrimSuffix(labels, "}") + fmt.Sprintf(",%s=%q}", key, value)
}
