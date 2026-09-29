package collector

import (
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"strings"
	"testing"
	"time"
)

func TestComponentIngressContract(t *testing.T) {
	r, _ := componentmetrics.New("monitor")
	r.Observe("scrapes_total", time.Second, "component", "monitor-local", "scrape_success")
	body, _ := r.Snapshot()
	if _, err := ParseComponent("monitor", body); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"family":        string(body) + "# TYPE extra gauge\nextra 1\n",
		"label key":     strings.Replace(string(body), `dependency="router"`, `dependency="router",source="backend"`, 1),
		"label value":   strings.Replace(string(body), `dependency="router"`, `dependency="private-host"`, 1),
		"duplicate":     string(body) + "gopulse_monitor_event_queue_length 0\n",
		"infinite":      strings.Replace(string(body), "gopulse_monitor_event_queue_length 0", "gopulse_monitor_event_queue_length +Inf", 1),
		"unpaired":      strings.Replace(string(body), "gopulse_monitor_scrape_duration_seconds_total{scraped_producer_kind=\"component\",scraped_target_id=\"monitor-local\",result=\"scrape_success\"} 1\n", "", 1),
		"sample budget": string(body) + strings.Repeat("gopulse_monitor_event_queue_length 0\n", 112),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseComponent("monitor", []byte(bad)); err == nil {
				t.Fatal("invalid component accepted")
			}
		})
	}
	// All five non-Backend producers legitimately have no counter tuples at boot.
	for _, id := range componentmetrics.Components {
		if id == "backend" {
			continue
		}
		r, _ := componentmetrics.New(id)
		b, _ := r.Snapshot()
		if _, err := ParseComponent(id, b); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
}

func TestComponentCatalogIncludesEveryExplicitObservabilityReplica(t *testing.T) {
	t.Setenv("ROUTER_ENDPOINTS", "router,router-2")
	t.Setenv("MARSHALLER_ENDPOINTS", "marshaller,marshaller-2")
	spec, ok := componentmetrics.Catalog("monitor")
	if !ok {
		t.Fatal("monitor catalog missing")
	}
	seen := map[string]bool{}
	for _, family := range spec.Families {
		if family.Name != "gopulse_monitor_scrapes_total" {
			continue
		}
		for _, tuple := range family.Tuples {
			if len(tuple) >= 3 {
				seen[tuple[0]+":"+tuple[1]+":"+tuple[2]] = true
			}
		}
	}
	for _, target := range []string{"component:router-local", "component:router-2-local", "component:marshaller-local", "component:marshaller-2-local"} {
		if !seen[target+":scrape_success"] {
			t.Fatalf("target %s missing from monitor catalog", target)
		}
	}
}

func TestBackendDistributionIngressRequiresFixedBucketsAndLabels(t *testing.T) {
	metrics, err := componentmetrics.NewBackend(componentmetrics.BackendRoutes())
	if err != nil {
		t.Fatal(err)
	}
	metrics.SetHTTPConcurrencyLimit(128)
	metrics.ObserveOutbox(0, 0, nil)
	metrics.ObserveRequest("GET", "/health", 200, 250*time.Millisecond)
	body, ok := metrics.Snapshot()
	if !ok {
		t.Fatal("backend snapshot unavailable")
	}
	if _, err := ParseComponent("backend", body); err != nil {
		t.Fatalf("valid backend distribution rejected: %v", err)
	}
	missing := strings.Replace(string(body), "gopulse_backend_http_request_duration_seconds_bucket{method=\"GET\",route=\"/health\",status_class=\"2xx\",le=\"0.25\"} 1\n", "", 1)
	if _, err := ParseComponent("backend", []byte(missing)); err == nil {
		t.Fatal("incomplete backend bucket distribution accepted")
	}
	foreign := strings.Replace(string(body), `le="0.25"`, `le="private"`, 1)
	if _, err := ParseComponent("backend", []byte(foreign)); err == nil {
		t.Fatal("foreign backend bucket label accepted")
	}
}
