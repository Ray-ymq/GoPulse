package instrumentation

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProducerUsesStandardRegistryAndBoundsLabels(t *testing.T) {
	for id := range specs {
		r, err := New(id)
		if err != nil {
			t.Fatal(err)
		}
		r.Set("dependency_up", 1, "untrusted-secret-label")
		var selected Family
		for _, f := range r.spec.Families {
			if f.Kind == "counter" && f.Pair != "" && f.Unit == "count" && f.Distribution == nil && len(f.Tuples) > 0 {
				selected = f
				break
			}
		}
		if selected.Name == "" {
			t.Fatal("no observation family")
		}
		suffix := strings.TrimPrefix(selected.Name, Prefix(id))
		tuple := selected.Tuples[0]
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				for range 50 {
					r.Observe(suffix, time.Second, tuple...)
				}
			})
		}
		wg.Wait()
		body, ok := r.Snapshot()
		if !ok || !strings.Contains(string(body), "# HELP go_goroutines") || strings.Contains(string(body), "untrusted-secret-label") {
			t.Fatal("invalid standard registry snapshot")
		}
		metrics, err := r.registry.Gather()
		if err != nil {
			t.Fatal(err)
		}
		seen := false
		for _, family := range metrics {
			if family.GetName() == selected.Name {
				for _, m := range family.Metric {
					if m.Counter.GetValue() == 400 {
						seen = true
					}
				}
			}
		}
		if !seen {
			t.Fatal("concurrent observations lost")
		}
	}
}
