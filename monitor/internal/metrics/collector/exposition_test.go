package collector

import (
	"net/http"
	"strings"
	"testing"
)

func TestNewHistogramAndSummaryCanBeCollected(t *testing.T) {
	body := `# TYPE gopulse_mysql_up gauge
gopulse_mysql_up 1
# TYPE gopulse_mysql_query_seconds histogram
gopulse_mysql_query_seconds_bucket{node="db-2",le="0.3"} 2
gopulse_mysql_query_seconds_bucket{node="db-2",le="+Inf"} 3
gopulse_mysql_query_seconds_sum{node="db-2"} 0.7
gopulse_mysql_query_seconds_count{node="db-2"} 3
# TYPE gopulse_mysql_query_size_bytes summary
gopulse_mysql_query_size_bytes{node="db-2",quantile="0.5"} 128
gopulse_mysql_query_size_bytes_sum{node="db-2"} 512
gopulse_mysql_query_size_bytes_count{node="db-2"} 3
`
	status, samples, err := parseSource("mysql", http.StatusOK, []byte(body))
	if err != nil || status != "success" || len(samples) != 8 {
		t.Fatalf("dynamic distribution rejected: %s, %#v, %v", status, samples, err)
	}
	for _, sample := range samples {
		if sample.Name == "gopulse_mysql_query_seconds_bucket" && sample.Labels["le"] == "0.3" && sample.Labels["node"] != "db-2" {
			t.Fatal("node dimension lost")
		}
	}
	for name, bad := range map[string]string{
		"non-cumulative":        strings.Replace(body, `le="0.3"} 2`, `le="0.3"} 4`, 1),
		"invalid quantile":      strings.Replace(body, `quantile="0.5"`, `quantile="1.5"`, 1),
		"duplicate empty label": body + "gopulse_mysql_extra{node=\"\",node=\"db-2\"} 1\n",
		"foreign declaration":   body + "# TYPE gopulse_redis_new gauge\n",
		"spoofed producer":      strings.Replace(body, `node="db-2"`, `producer_id="redis-exporter"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseSource("mysql", 200, []byte(bad)); err == nil {
				t.Fatal("invalid distribution accepted")
			}
		})
	}
}

func TestComponentTransportAcceptsUnregisteredFamiliesAndDimensions(t *testing.T) {
	body := []byte("# TYPE gopulse_business_worker_new_depth gauge\ngopulse_business_worker_new_depth{queue=\"jobs\",node=\"worker-2\"} 7\n")
	samples, err := ParseComponent("business-worker", body)
	if err != nil || len(samples) != 1 || samples[0].Labels["queue"] != "jobs" {
		t.Fatalf("new component metric rejected: %#v, %v", samples, err)
	}
}
