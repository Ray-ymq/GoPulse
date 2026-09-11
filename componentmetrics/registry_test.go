package componentmetrics

import (
	"strings"
	"testing"
	"time"
)

func TestFixedCatalogBudgetsAndInitialState(t *testing.T) {
	budgets := map[string]int{"backend": 547, "business-worker": 37, "search-indexer": 24, "monitor": 112, "router": 112, "marshaller": 260}
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
