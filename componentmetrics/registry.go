package componentmetrics

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"
)

type cell struct {
	value  atomic.Pointer[completedRequest]
	gauge  atomic.Uint64
	labels string
	values []string
}
type familyState struct {
	definition Family
	cells      map[string]*cell
	order      []*cell
}
type Registry struct {
	spec     Spec
	families map[string]*familyState
}

var active atomic.Pointer[Registry]
var activeBackend atomic.Pointer[Backend]

func Active() *Registry         { return active.Load() }
func Install(r *Registry)       { active.Store(r) }
func InstallBackend(b *Backend) { activeBackend.Store(b) }
func BackendActive() *Backend   { return activeBackend.Load() }
func Dependency(name string, err error) {
	if b := activeBackend.Load(); b != nil {
		b.ObserveDependency(name, err)
	}
	if r := Active(); r != nil {
		v := float64(1)
		if err != nil {
			v = 0
		}
		r.Set("dependency_up", v, name)
	}
}
func New(id string) (*Registry, error) {
	if id == "backend" {
		return nil, errors.New("backend requires outbox-aware metrics state")
	}
	spec, ok := Catalog(id)
	if !ok {
		return nil, errors.New("unknown component metrics catalog")
	}
	r := &Registry{spec: spec, families: make(map[string]*familyState)}
	for _, f := range spec.Families {
		state := &familyState{definition: f, cells: make(map[string]*cell)}
		for _, values := range f.Tuples {
			c := &cell{labels: labelText(f.Keys, values), values: values}
			if strings.HasSuffix(f.Name, "dependency_up") {
				c.gauge.Store(math.Float64bits(-1))
			}
			state.cells[tupleKey(values)] = c
			state.order = append(state.order, c)
		}
		r.families[f.Name] = state
	}
	return r, nil
}
func (r *Registry) lookup(suffix string, values []string) (*familyState, *cell) {
	if r == nil {
		return nil, nil
	}
	f := r.families[Prefix(r.spec.ID)+suffix]
	if f == nil {
		return nil, nil
	}
	return f, f.cells[tupleKey(values)]
}
func (r *Registry) Set(suffix string, value float64, values ...string) {
	f, c := r.lookup(suffix, values)
	if c == nil || math.IsNaN(value) || math.IsInf(value, 0) || f.definition.Kind != "gauge" {
		return
	}
	if !validValue(f.definition, value) {
		return
	}
	c.gauge.Store(math.Float64bits(value))
}
func (r *Registry) Add(suffix string, delta float64, values ...string) {
	_, c := r.lookup(suffix, values)
	if c == nil || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return
	}
	for {
		old := c.gauge.Load()
		v := math.Float64frombits(old) + delta
		if v < 0 || math.IsInf(v, 0) {
			return
		}
		if c.gauge.CompareAndSwap(old, math.Float64bits(v)) {
			return
		}
	}
}
func (r *Registry) Observe(suffix string, elapsed time.Duration, values ...string) {
	f, c := r.lookup(suffix, values)
	if c == nil || elapsed < 0 || f.definition.Unit != "count" || f.definition.Pair == "" {
		return
	}
	next := &completedRequest{}
	for {
		old := c.value.Load()
		*next = completedRequest{count: 1, seconds: elapsed.Seconds()}
		if old != nil {
			next.count += old.count
			next.seconds += old.seconds
		}
		if c.value.CompareAndSwap(old, next) {
			return
		}
	}
}
func (r *Registry) Snapshot() ([]byte, bool) {
	if r == nil {
		return nil, false
	}
	var b strings.Builder
	for _, spec := range r.spec.Families {
		fmt.Fprintf(&b, "# TYPE %s %s\n", spec.Name, spec.Kind)
	}
	for _, spec := range r.spec.Families {
		if spec.Pair != "" && spec.Unit == "seconds" {
			continue
		}
		for _, c := range r.families[spec.Name].order {
			if spec.Pair != "" {
				v := c.value.Load()
				if v == nil {
					continue
				}
				fmt.Fprintf(&b, "%s%s %d\n%s%s %g\n", spec.Name, c.labels, v.count, spec.Pair, c.labels, v.seconds)
			} else {
				fmt.Fprintf(&b, "%s%s %g\n", spec.Name, c.labels, math.Float64frombits(c.gauge.Load()))
			}
		}
	}
	if b.Len() > r.spec.MaxBodyBytes {
		return nil, false
	}
	return []byte(b.String()), true
}
