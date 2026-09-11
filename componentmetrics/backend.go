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
type requestSeries struct {
	labels string
	value  atomic.Pointer[completedRequest]
}
type backlog struct {
	pending       int64
	oldestSeconds float64
	valid         bool
}

// Backend stores a fixed set of series slots allocated at router construction.
// Hot-path updates perform no I/O, acquire no mutex, and allocate only a single
// fixed-size count/duration pair. One atomic publication keeps the pair coherent.
type Backend struct {
	requests     map[httpTuple]*requestSeries
	ordered      []*requestSeries
	dependencies [4]atomic.Int32
	outbox       atomic.Pointer[backlog]
	lastPublish  atomic.Int64
}

var backendMethods = [...]string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE", "unknown"}
var backendDependencies = [...]string{"mysql", "redis", "rabbitmq", "elasticsearch"}

const BackendFamilies = 6

// BackendMaxRoutes guards accidental unbounded registration, not client input.
const BackendMaxRoutes = 64
const BackendMaxBodyBytes = 256 * 1024

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
	next := &completedRequest{}
	for {
		old := series.value.Load()
		*next = completedRequest{count: 1, seconds: elapsed.Seconds()}
		if old != nil {
			next.count += old.count
			next.seconds += old.seconds
		}
		if series.value.CompareAndSwap(old, next) {
			return
		}
	}
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

// MaxSamples includes both counter families for every permitted tuple, four
// dependencies, and three outbox gauges. No client value can increase it.
func (b *Backend) MaxSamples() int { return len(b.ordered)*2 + 7 }

func (b *Backend) Snapshot() ([]byte, bool) {
	state := b.outbox.Load()
	if state == nil || !state.valid {
		return nil, false
	}
	var out strings.Builder
	out.WriteString("# TYPE gopulse_backend_http_requests_total counter\n# TYPE gopulse_backend_http_request_duration_seconds_total counter\n")
	for _, series := range b.ordered {
		value := series.value.Load()
		if value == nil {
			continue
		}
		fmt.Fprintf(&out, "gopulse_backend_http_requests_total%s %d\ngopulse_backend_http_request_duration_seconds_total%s %s\n", series.labels, value.count, series.labels, strconv.FormatFloat(value.seconds, 'g', -1, 64))
	}
	fmt.Fprintf(&out, "# TYPE gopulse_backend_outbox_pending gauge\ngopulse_backend_outbox_pending %d\n# TYPE gopulse_backend_outbox_oldest_age_seconds gauge\ngopulse_backend_outbox_oldest_age_seconds %s\n# TYPE gopulse_backend_outbox_last_publish_success_timestamp_seconds gauge\ngopulse_backend_outbox_last_publish_success_timestamp_seconds %d\n# TYPE gopulse_backend_dependency_up gauge\n", state.pending, strconv.FormatFloat(state.oldestSeconds, 'g', -1, 64), b.lastPublish.Load())
	for i, name := range backendDependencies {
		fmt.Fprintf(&out, "gopulse_backend_dependency_up{dependency=%q} %d\n", name, b.dependencies[i].Load())
	}
	if out.Len() > BackendMaxBodyBytes {
		return nil, false
	}
	return []byte(out.String()), true
}
