package metricquery

import (
	"net/url"
	"strings"
	"testing"
)

func TestSelfMonitoringIsNotInProductQueryOrAlertCatalog(t *testing.T) {
	for _, name := range []string{"gopulse_backend_dependency_up", "gopulse_backend_http_request_duration_seconds_bucket", "gopulse_backend_alert_evaluation_known", "gopulse_marshaller_partition_ownership", "gopulse_monitor_event_queue_length", "gopulse_business_worker_messages_total", "gopulse_search_indexer_messages_total"} {
		if _, err := ParseOptions(url.Values{"metric": {name}}); err == nil || QueryExpression(name) != "" {
			t.Fatalf("accepted retired product metric %s", name)
		}
	}
	for _, d := range AlertCatalog() {
		for _, prefix := range []string{"gopulse_backend_", "gopulse_monitor_", "gopulse_marshaller_", "gopulse_business_worker_", "gopulse_search_indexer_"} {
			if strings.HasPrefix(d.Metric, prefix) {
				t.Fatal("retired alert definition", d.Metric)
			}
		}
	}
}
