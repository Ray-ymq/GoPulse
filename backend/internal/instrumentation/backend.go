package instrumentation

import (
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Route struct{ Method, Template string }

const BackendMaxRoutes = 96
const BackendMaxBodyBytes = 1 << 20
const BackendFamilies = 28

var backendMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE", "unknown"}
var latencyBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10}

func BackendLatencyBuckets() []string {
	return []string{"0.005", "0.01", "0.025", "0.05", "0.1", "0.25", "0.5", "1", "2", "5", "10", "+Inf"}
}

type Backend struct {
	state   *Registry
	routes  map[Route]bool
	latency *prometheus.HistogramVec
}

var activeBackend atomic.Pointer[Backend]

func InstallBackend(b *Backend) { activeBackend.Store(b) }
func BackendActive() *Backend   { return activeBackend.Load() }
func init() {
	dependencyObserver = func(name string, err error) {
		if b := BackendActive(); b != nil {
			b.ObserveDependency(name, err)
		}
	}
	nilGaugeObserver = func(suffix string, v float64, values []string) {
		if b := BackendActive(); b != nil && len(values) == 1 {
			b.ObserveAlert(suffix, v, values[0])
		}
	}
}
func knownMethod(method string) bool {
	for _, m := range backendMethods {
		if m == method {
			return true
		}
	}
	return false
}
func NewBackend(routes []Route) (*Backend, error) {
	if len(routes) > BackendMaxRoutes {
		return nil, errors.New("too many metric routes")
	}
	state, err := New("backend")
	if err != nil {
		return nil, err
	}
	b := &Backend{state: state, routes: map[Route]bool{}, latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "gopulse_backend_http_request_duration_seconds", Help: "Backend request latency in seconds.", Buckets: latencyBuckets}, []string{"method", "route", "status_class"})}
	for _, route := range routes {
		if !knownMethod(route.Method) || route.Method == "unknown" || !strings.HasPrefix(route.Template, "/") || len(route.Template) > 160 || strings.ContainsAny(route.Template, "\"\\\r\n?{}") {
			return nil, errors.New("invalid metric route")
		}
		b.routes[route] = true
	}
	// Freeze allowed tuples using the actual registered route inventory.
	for _, name := range []string{"http_requests_total", "http_request_duration_seconds_total"} {
		f := state.families[Prefix("backend")+name]
		f.allowed = map[string]bool{}
		for route := range b.routes {
			for class := 1; class <= 5; class++ {
				f.allowed[tupleKey([]string{route.Method, route.Template, strconv.Itoa(class) + "xx"})] = true
			}
		}
		for _, method := range backendMethods {
			for class := 1; class <= 5; class++ {
				f.allowed[tupleKey([]string{method, "_unmatched", strconv.Itoa(class) + "xx"})] = true
			}
		}
	}
	state.registry.MustRegister(b.latency)
	b.clearOutbox()
	return b, nil
}
func (b *Backend) ObserveRequest(method, template string, status int, elapsed time.Duration) {
	if b == nil || elapsed < 0 || status < 100 || status >= 600 {
		return
	}
	if !knownMethod(method) {
		method = "unknown"
	}
	if !b.routes[Route{method, template}] {
		template = "_unmatched"
	}
	class := strconv.Itoa(status/100) + "xx"
	b.state.Observe("http_requests_total", elapsed, method, template, class)
	b.latency.WithLabelValues(method, template, class).Observe(elapsed.Seconds())
}
func (b *Backend) SetHTTPConcurrencyLimit(n int) {
	if b != nil && n >= 0 {
		b.state.Set("http_concurrency_limit", float64(n))
	}
}
func (b *Backend) ObserveHTTPStarted() {
	if b != nil {
		b.state.Add("http_requests_in_flight", 1)
	}
}
func (b *Backend) ObserveHTTPFinished() {
	if b != nil {
		b.state.Add("http_requests_in_flight", -1)
	}
}
func (b *Backend) ObserveHTTPRejected() {
	if b != nil {
		b.state.Add("http_rejected_total", 1)
	}
}
func (b *Backend) ObserveDependency(name string, err error) {
	if b != nil {
		v := float64(1)
		if err != nil {
			v = 0
		}
		b.state.Set("dependency_up", v, name)
	}
}
func (b *Backend) ObserveAlert(suffix string, value float64, source string) {
	if b != nil && (suffix == "alert_evaluation_known" || suffix == "alert_last_success_timestamp_seconds") {
		if suffix == "alert_evaluation_known" && value != 0 && value != 1 {
			return
		}
		b.state.Set(suffix, value, source)
	}
}
func (b *Backend) clearOutbox() {
	for _, suffix := range []string{"outbox_pending", "outbox_oldest_age_seconds"} {
		b.state.families[Prefix("backend")+suffix].gauges.DeleteLabelValues()
	}
}
func (b *Backend) ObserveOutbox(pending int64, oldest time.Duration, err error) {
	if b == nil {
		return
	}
	b.ObserveDependency("mysql", err)
	if err != nil || pending < 0 || oldest < 0 {
		b.clearOutbox()
		return
	}
	b.state.Set("outbox_pending", float64(pending))
	b.state.Set("outbox_oldest_age_seconds", oldest.Seconds())
}
func (b *Backend) ObservePublish(at time.Time, err error) {
	if b != nil {
		b.ObserveDependency("rabbitmq", err)
		if err == nil {
			b.state.Set("outbox_last_publish_success_timestamp_seconds", float64(at.Unix()))
		}
	}
}
func (b *Backend) ObserveFreshness(stage, result string, elapsed time.Duration) {
	if b != nil {
		b.state.Observe("freshness_events_total", elapsed, stage, result)
	}
}
func (b *Backend) ObserveTraceContextInvalid() {
	if b != nil {
		b.state.Add("trace_context_invalid_total", 1)
	}
}
func (b *Backend) ObserveCacheRequest(result string) {
	if b != nil {
		b.state.Add("cache_requests_total", 1, result)
	}
}
func (b *Backend) ObserveCacheOrigin(result string) { b.ObserveCacheOriginDuration(result, 0) }
func (b *Backend) ObserveCacheOriginDuration(result string, elapsed time.Duration) {
	if b != nil {
		b.state.Observe("cache_origin_total", elapsed, result)
	}
}
func (b *Backend) ObserveRateLimit(policy, mode, result string) {
	if b != nil {
		b.state.Add("rate_limit_decisions_total", 1, policy, mode, result)
	}
}
func (b *Backend) ObserveResourceRejected(kind, reason string) {
	if b != nil {
		b.state.Add("resource_rejected_total", 1, kind, reason)
	}
}
func (b *Backend) ObserveDBOperation(op, result string, elapsed time.Duration) {
	if b != nil {
		b.state.Observe("db_operations_total", elapsed, op, result)
	}
}
func (b *Backend) ObserveRebuildState(active, waiters, registered int) {
	if b != nil && active >= 0 && waiters >= 0 && registered >= 0 {
		b.state.Set("cache_rebuild_in_flight", float64(active))
		b.state.Set("cache_waiters", float64(waiters))
		b.state.Set("cache_registered_waiters", float64(registered))
	}
}
func (b *Backend) MaxSamples() int {
	if b == nil {
		return 0
	}
	return len(b.routes)*5*16 + 1000
}
func (b *Backend) Snapshot() ([]byte, bool) {
	if b == nil {
		return nil, false
	}
	return b.state.Snapshot()
}
