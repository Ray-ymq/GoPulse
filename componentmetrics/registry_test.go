package componentmetrics

import (
	"strings"
	"testing"
	"time"
)

func TestFixedCatalogBudgetsAndInitialState(t *testing.T) {
	budgets := map[string]int{"backend": 5637, "business-worker": 42, "search-indexer": 37, "monitor": 112, "router": 120, "marshaller": 362}
	for _, id := range Components {
		spec, _ := Catalog(id)
		if spec.MaxSamples != budgets[id] {
			t.Errorf("%s sample budget %d", id, spec.MaxSamples)
		}
		if id == "backend" {
			continue
		}
		r, err := New(id)
		if err != nil {
			t.Fatal(err)
		}
		body, ok := r.Snapshot()
		if !ok || len(body) > spec.MaxBodyBytes {
			t.Fatal("invalid initial body")
		}
		if !strings.Contains(string(body), "_dependency_up{dependency=") {
			t.Fatal("missing unknown dependency")
		}
	}
	r, _ := New("monitor")
	r.Observe("scrapes_total", time.Second, "component", "monitor-local", "scrape_success")
	body, _ := r.Snapshot()
	if !strings.Contains(string(body), `scraped_producer_kind="component",scraped_target_id="monitor-local"`) {
		t.Fatal("scraped identity lost")
	}
}

func TestValidateFixedDistributionRequiresCompleteCumulativeBuckets(t *testing.T) {
	spec, ok := Catalog("backend")
	if !ok {
		t.Fatal("backend catalog missing")
	}
	samples := make([]Sample, 0)
	for _, family := range spec.Families {
		if !family.Required {
			continue
		}
		for _, tuple := range family.Tuples {
			samples = append(samples, Sample{Name: family.Name, Kind: family.Kind, Labels: Labels(family, tuple), Value: 0})
		}
	}
	var bucket, count, sum Family
	for _, family := range spec.Families {
		if family.Distribution == nil {
			continue
		}
		switch family.Distribution.Role {
		case "bucket":
			bucket = family
		case "count":
			count = family
		case "sum":
			sum = family
		}
	}
	base := count.Tuples[0]
	for i, le := range bucket.Distribution.Buckets {
		value := float64(0)
		if i >= len(bucket.Distribution.Buckets)-1 {
			value = 1
		}
		labels := Labels(bucket, append(append([]string(nil), base...), le))
		samples = append(samples, Sample{Name: bucket.Name, Kind: bucket.Kind, Labels: labels, Value: value})
	}
	samples = append(samples,
		Sample{Name: count.Name, Kind: count.Kind, Labels: Labels(count, base), Value: 1},
		Sample{Name: sum.Name, Kind: sum.Kind, Labels: Labels(sum, base), Value: 0.25},
	)
	if err := Validate("backend", samples); err != nil {
		t.Fatalf("valid distribution rejected: %v", err)
	}
	missing := append([]Sample(nil), samples[:len(samples)-1]...)
	if err := Validate("backend", missing); err == nil {
		t.Fatal("incomplete distribution accepted")
	}
	invalid := append([]Sample(nil), samples...)
	invalid[len(invalid)-3].Labels["le"] = "0.3"
	if err := Validate("backend", invalid); err == nil {
		t.Fatal("unknown bucket accepted")
	}
}
