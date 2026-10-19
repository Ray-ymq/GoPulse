package instrumentation

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/common/expfmt"
)

type familyState struct {
	definition Family
	gauges     *prometheus.GaugeVec
	counters   *prometheus.CounterVec
	allowed    map[string]bool
	values     map[string]float64
}
type Registry struct {
	spec     Spec
	registry *prometheus.Registry
	families map[string]*familyState
	mu       sync.Mutex
}

var active atomic.Pointer[Registry]
var dependencyObserver = func(string, error) {}
var nilGaugeObserver = func(string, float64, []string) {}

func Active() *Registry   { return active.Load() }
func Install(r *Registry) { active.Store(r) }
func Dependency(name string, err error) {
	dependencyObserver(name, err)
	v := float64(1)
	if err != nil {
		v = 0
	}
	Active().Set("dependency_up", v, name)
}
func New(id string) (*Registry, error) {
	spec, ok := Catalog(id)
	if !ok {
		return nil, errors.New("unknown producer")
	}
	r := &Registry{spec: spec, registry: prometheus.NewRegistry(), families: map[string]*familyState{}}
	r.registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	for _, f := range spec.Families {
		if f.Distribution != nil {
			continue
		}
		state := &familyState{definition: f, allowed: map[string]bool{}, values: map[string]float64{}}
		if f.Kind == "gauge" {
			state.gauges = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: f.Name, Help: f.Name}, f.Keys)
			r.registry.MustRegister(state.gauges)
		} else {
			state.counters = prometheus.NewCounterVec(prometheus.CounterOpts{Name: f.Name, Help: f.Name}, f.Keys)
			r.registry.MustRegister(state.counters)
		}
		for _, tuple := range f.Tuples {
			key := tupleKey(tuple)
			state.allowed[key] = true
			if state.gauges != nil {
				v := float64(0)
				if strings.HasSuffix(f.Name, "dependency_up") {
					v = -1
				}
				state.gauges.WithLabelValues(tuple...).Set(v)
				state.values[key] = v
			} else if f.Required {
				state.counters.WithLabelValues(tuple...).Add(0)
			}
		}
		r.families[f.Name] = state
	}
	return r, nil
}
func (r *Registry) lookup(suffix string, values []string) *familyState {
	if r == nil {
		return nil
	}
	f := r.families[Prefix(r.spec.ID)+suffix]
	if f == nil || !f.allowed[tupleKey(values)] {
		return nil
	}
	return f
}
func (r *Registry) Set(suffix string, value float64, values ...string) {
	if r == nil {
		nilGaugeObserver(suffix, value, values)
		return
	}
	f := r.lookup(suffix, values)
	if f == nil || f.gauges == nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	if strings.HasSuffix(f.definition.Name, "dependency_up") {
		if value != -1 && value != 0 && value != 1 {
			return
		}
	} else if value < 0 {
		return
	}
	if (f.definition.Unit == "count" || f.definition.Unit == "bytes" || f.definition.Unit == "unix_seconds") && value != math.Trunc(value) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f.gauges.WithLabelValues(values...).Set(value)
	f.values[tupleKey(values)] = value
}
func (r *Registry) Add(suffix string, delta float64, values ...string) {
	f := r.lookup(suffix, values)
	if f == nil || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if f.counters != nil {
		if delta >= 0 {
			f.counters.WithLabelValues(values...).Add(delta)
		}
		return
	}
	key := tupleKey(values)
	next := f.values[key] + delta
	if next < 0 || math.IsInf(next, 0) {
		return
	}
	f.values[key] = next
	f.gauges.WithLabelValues(values...).Set(next)
}
func (r *Registry) Observe(suffix string, elapsed time.Duration, values ...string) {
	f := r.lookup(suffix, values)
	if f == nil || f.counters == nil || f.definition.Pair == "" || f.definition.Unit != "count" || elapsed < 0 {
		return
	}
	pair := r.families[f.definition.Pair]
	if pair == nil || pair.counters == nil {
		return
	}
	f.counters.WithLabelValues(values...).Inc()
	pair.counters.WithLabelValues(values...).Add(elapsed.Seconds())
}
func (r *Registry) Snapshot() ([]byte, bool) {
	if r == nil {
		return nil, false
	}
	families, err := r.registry.Gather()
	if err != nil {
		return nil, false
	}
	var out bytes.Buffer
	for _, f := range families {
		if _, err := expfmt.MetricFamilyToText(&out, f); err != nil {
			return nil, false
		}
	}
	if out.Len() > 1<<20 {
		return nil, false
	}
	return out.Bytes(), true
}
