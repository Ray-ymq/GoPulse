package k6

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSummaryPhaseClassifiesErrorsAndAggregatesRouteTemplates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.json")
	content := `{
  "metrics": {
    "business_requests{phase:measurement,category:read,route:GET /api/v1/posts,method:GET}": {"count": 95},
    "business_latency{phase:measurement,category:read,route:GET /api/v1/posts,method:GET}": {"p(95)": 800, "p(99)": 1200},
    "business_success_rate{phase:measurement}": {"value": 0.99},
    "errors_429{phase:measurement}": {"count": 2},
    "errors_503{phase:measurement}": {"count": 1},
    "errors_timeout{phase:measurement}": {"count": 1},
    "errors_transport{phase:measurement}": {"count": 1},
    "errors_unexpected{phase:measurement}": {"count": 0},
    "delete_succeeded_measurement": {"count": 3},
    "delete_already_absent_measurement": {"count": 7},
    "dropped_iterations{phase:measurement}": {"count": 5},
    "health_success_rate": {"value": 1}
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	metrics, err := ReadSummaryPhase(path, "measurement", 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.DeleteSucceeded != 3 || metrics.DeleteAlreadyAbsent != 7 || metrics.Requests != 95 || metrics.ActualRequestRate != 95 || metrics.DroppedIterations != 5 || metrics.Errors.Rejected429 != 2 || metrics.Errors.Rejected503 != 1 || metrics.Errors.Timeouts != 1 || metrics.Errors.TransportErrors != 1 {
		t.Fatalf("metrics=%+v", metrics)
	}
	if len(metrics.Routes) != 1 || metrics.Routes[0].Template != "GET /api/v1/posts" || metrics.Routes[0].P95MS != 800 || metrics.Routes[0].P99MS != 1200 {
		t.Fatalf("routes=%+v", metrics.Routes)
	}
}

func TestActualWindowOverridesPlannedDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.json")
	data := `{"metrics":{"business_requests_stage0_measurement":{"count":100},"business_success_rate_stage0_measurement":{"rate":0.9},"window_seconds_stage0_measurement":{"value":2}}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	metrics, err := ReadSummaryPhase(path, "stage0_measurement", 50, 300)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.ActualSeconds != 2 || metrics.ActualRequestRate != 50 || metrics.SuccessfulThroughput != 45 {
		t.Fatalf("metrics=%+v", metrics)
	}
	absent, err := ReadSummaryPhase(path, "stage1_measurement", 100, 0)
	if err != nil || absent.Requests != 0 || absent.ActualSeconds != 0 {
		t.Fatalf("未执行档位混入全局计数: %+v %v", absent, err)
	}
}
