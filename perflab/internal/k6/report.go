package k6

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	StatusStable               = "stable"
	StatusUnstable             = "unstable"
	StatusCapacityInsufficient = "capacity_insufficient"
	StatusEnvironmentFailure   = "environment_failure"
	StatusIncomplete           = "incomplete"
	StatusNotExecuted          = "not_executed"
)

type Report struct {
	SchemaVersion   string          `json:"schema_version"`
	Mode            Mode            `json:"mode"`
	RunID           string          `json:"run_id"`
	ConfigPath      string          `json:"config_path"`
	ConfigSHA256    string          `json:"config_sha256"`
	StartedAt       time.Time       `json:"started_at"`
	FinishedAt      time.Time       `json:"finished_at"`
	ExecutionStatus string          `json:"execution_status"`
	Thresholds      Thresholds      `json:"thresholds"`
	Stages          []StageResult   `json:"stages"`
	Boundary        BoundarySummary `json:"boundary"`
	Resources       ResourceMetrics `json:"resources"`
}

type BoundarySummary struct {
	StableRPS         float64 `json:"stable_rps,omitempty"`
	FirstUnstableRPS  float64 `json:"first_unstable_rps,omitempty"`
	BoundaryFound     bool    `json:"boundary_found"`
	CapacityJudgement string  `json:"capacity_judgement"`
}

type StageResult struct {
	Name           string         `json:"name"`
	TargetRPS      float64        `json:"target_rps"`
	Status         string         `json:"status"`
	Classification []string       `json:"classification"`
	Warmup         WindowMetrics  `json:"warmup"`
	Measurement    WindowMetrics  `json:"measurement"`
	Recovery       RecoveryResult `json:"recovery"`
	K6ExitCode     int            `json:"k6_exit_code"`
	K6SummaryPath  string         `json:"k6_summary_path,omitempty"`
	Error          string         `json:"error,omitempty"`
}

type WindowMetrics struct {
	DeleteSucceeded      uint64         `json:"delete_succeeded"`
	DeleteAlreadyAbsent  uint64         `json:"delete_already_absent"`
	TargetRPS            float64        `json:"target_rps"`
	ActualRequestRate    float64        `json:"actual_request_rate"`
	SuccessfulThroughput float64        `json:"successful_throughput"`
	Requests             uint64         `json:"requests"`
	SuccessfulRequests   uint64         `json:"successful_requests"`
	P95MS                float64        `json:"p95_ms"`
	P99MS                float64        `json:"p99_ms"`
	ErrorRate            float64        `json:"error_rate"`
	HealthFailureRate    float64        `json:"health_failure_rate"`
	DroppedIterations    uint64         `json:"dropped_iterations"`
	DroppedIterationRate float64        `json:"dropped_iteration_rate"`
	Errors               ErrorCounts    `json:"errors"`
	Routes               []RouteMetrics `json:"routes"`
}

type ErrorCounts struct {
	Rejected429      uint64 `json:"rejected_429"`
	Rejected503      uint64 `json:"rejected_503"`
	Timeouts         uint64 `json:"timeouts"`
	TransportErrors  uint64 `json:"transport_errors"`
	UnexpectedErrors uint64 `json:"unexpected_errors"`
}

type RouteMetrics struct {
	Template  string  `json:"template"`
	Category  string  `json:"category"`
	Method    string  `json:"method"`
	Requests  uint64  `json:"requests"`
	P95MS     float64 `json:"p95_ms"`
	P99MS     float64 `json:"p99_ms"`
	ErrorRate float64 `json:"error_rate"`
}

type RecoveryResult struct {
	Status         string  `json:"status"`
	Baseline       float64 `json:"baseline,omitempty"`
	After          float64 `json:"after,omitempty"`
	Recovered      bool    `json:"recovered"`
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	Error          string  `json:"error,omitempty"`
}

type ResourceMetrics struct {
	LoadCPUPercent *float64 `json:"load_cpu_percent,omitempty"`
	LoadRSSBytes   *uint64  `json:"load_rss_bytes,omitempty"`
	Source         string   `json:"source"`
}

type k6Summary struct {
	Metrics map[string]k6Metric `json:"metrics"`
}

type k6Metric struct {
	Values map[string]float64 `json:"values"`
}

func (metric *k6Metric) UnmarshalJSON(encoded []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return err
	}
	metric.Values = map[string]float64{}
	for key, value := range raw {
		var number float64
		if json.Unmarshal(value, &number) == nil {
			metric.Values[key] = number
		}
	}
	if value, ok := metric.Values["value"]; ok {
		metric.Values["rate"] = value
	}
	if _, ok := metric.Values["rate"]; !ok {
		passes, hasPasses := metric.Values["passes"]
		fails, hasFails := metric.Values["fails"]
		if hasPasses && hasFails && passes+fails > 0 {
			metric.Values["rate"] = passes / (passes + fails)
		}
	}
	return nil
}

func ReadSummary(path string) (WindowMetrics, error) {
	return ReadSummaryPhase(path, "", 0, 0)
}

func ReadSummaryPhase(path, phase string, target, seconds float64) (WindowMetrics, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return WindowMetrics{}, fmt.Errorf("read k6 summary: %w", err)
	}
	var summary k6Summary
	if err := json.Unmarshal(encoded, &summary); err != nil {
		return WindowMetrics{}, fmt.Errorf("decode k6 summary: %w", err)
	}
	requests := metricCountPhase(summary.Metrics, "business_requests", phase)
	if requests == 0 {
		requests = metricCountPhase(summary.Metrics, "http_reqs", phase)
	}
	failedRate := 0.0
	if _, ok := metricForPhase(summary.Metrics, "business_success_rate", phase); ok {
		failedRate = 1 - metricRatePhase(summary.Metrics, "business_success_rate", phase)
	} else {
		failedRate = metricRatePhase(summary.Metrics, "business_failure_rate", phase)
	}
	if failedRate < 0 {
		failedRate = 0
	}
	successful := uint64(math.Round(float64(requests) * (1 - failedRate)))
	dropped := metricCountPhase(summary.Metrics, "dropped_iterations", phase)
	denominator := requests + dropped
	droppedRate := 0.0
	if denominator > 0 {
		droppedRate = float64(dropped) / float64(denominator)
	}
	metrics := WindowMetrics{
		DeleteSucceeded:      metricCountPhase(summary.Metrics, "delete_succeeded", phase),
		DeleteAlreadyAbsent:  metricCountPhase(summary.Metrics, "delete_already_absent", phase),
		TargetRPS:            target,
		Requests:             requests,
		SuccessfulRequests:   successful,
		P95MS:                metricValuePhase(summary.Metrics, "business_latency", "p(95)", phase),
		P99MS:                metricValuePhase(summary.Metrics, "business_latency", "p(99)", phase),
		ErrorRate:            failedRate,
		HealthFailureRate:    healthFailureRate(summary.Metrics),
		DroppedIterations:    dropped,
		DroppedIterationRate: droppedRate,
		Errors: ErrorCounts{
			Rejected429:      metricCountPhase(summary.Metrics, "errors_429", phase),
			Rejected503:      metricCountPhase(summary.Metrics, "errors_503", phase),
			Timeouts:         metricCountPhase(summary.Metrics, "errors_timeout", phase),
			TransportErrors:  metricCountPhase(summary.Metrics, "errors_transport", phase),
			UnexpectedErrors: metricCountPhase(summary.Metrics, "errors_unexpected", phase),
		},
	}
	if seconds > 0 {
		metrics = metrics.WithTarget(target, seconds)
	}
	fillRouteMetricsPhase(&metrics, summary, phase)
	return metrics, nil
}

func healthFailureRate(metrics map[string]k6Metric) float64 {
	if _, ok := metricForPhase(metrics, "health_success_rate", ""); ok {
		return 1 - metricRatePhase(metrics, "health_success_rate", "")
	}
	return metricRatePhase(metrics, "health_failure_rate", "")
}

func (metrics WindowMetrics) WithTarget(target, seconds float64) WindowMetrics {
	metrics.TargetRPS = target
	if seconds > 0 {
		metrics.ActualRequestRate = float64(metrics.Requests) / seconds
		metrics.SuccessfulThroughput = float64(metrics.SuccessfulRequests) / seconds
	}
	return metrics
}

func classify(metrics WindowMetrics, recovery RecoveryResult, thresholds Thresholds, requireRecovery bool) (string, []string) {
	reasons := []string{}
	if metrics.P95MS > thresholds.MaxP95MS {
		reasons = append(reasons, "p95_over_threshold")
	}
	if metrics.P99MS > thresholds.MaxP99MS {
		reasons = append(reasons, "p99_over_threshold")
	}
	if metrics.ErrorRate > thresholds.MaxErrorRate {
		reasons = append(reasons, "error_rate_over_threshold")
	}
	if metrics.TargetRPS > 0 && metrics.ActualRequestRate < metrics.TargetRPS*thresholds.MinAchievedRPSRatio {
		reasons = append(reasons, "achieved_rate_below_target")
	}
	if metrics.DroppedIterationRate > thresholds.MaxDroppedIterationRate {
		reasons = append(reasons, "load_generator_dropped_iterations")
	}
	if recovery.Status == "not_recovered" {
		reasons = append(reasons, "backlog_not_recovered")
	}
	if recovery.Status == "probe_failed" {
		reasons = append(reasons, "backlog_probe_failed")
	}
	if recovery.Status == "not_observed" {
		if requireRecovery {
			reasons = append(reasons, "backlog_not_observed")
		} else {
			return StatusCapacityInsufficient, []string{"backlog_not_observed"}
		}
	}
	if metrics.HealthFailureRate > 0 {
		reasons = append(reasons, "health_check_failed")
	}
	if len(reasons) == 0 {
		return StatusStable, reasons
	}
	if recovery.Status == "probe_failed" {
		return StatusEnvironmentFailure, reasons
	}
	if recovery.Status == "interrupted" {
		return StatusIncomplete, reasons
	}
	return StatusUnstable, reasons
}

func metricForPhase(metrics map[string]k6Metric, name, phase string) (k6Metric, bool) {
	if phase == "" {
		metric, ok := metrics[name]
		return metric, ok
	}
	if metric, ok := metrics[name+"_"+phase]; ok {
		return metric, true
	}
	for key, metric := range metrics {
		if !strings.HasPrefix(key, name+"{") {
			continue
		}
		tags := parseTags(key)
		if tags["phase"] == phase {
			return metric, true
		}
	}
	return k6Metric{}, false
}

func metricValuePhase(metrics map[string]k6Metric, name, value, phase string) float64 {
	metric, ok := metricForPhase(metrics, name, phase)
	if !ok && phase != "" {
		metric, ok = metricForPhase(metrics, name, "")
	}
	if !ok {
		return 0
	}
	return metric.Values[value]
}

func metricCountPhase(metrics map[string]k6Metric, name, phase string) uint64 {
	metric, ok := metricForPhase(metrics, name, phase)
	if !ok && phase != "" {
		metric, ok = metricForPhase(metrics, name, "")
	}
	if !ok {
		return 0
	}
	return uint64(math.Round(metric.Values["count"]))
}

func metricRatePhase(metrics map[string]k6Metric, name, phase string) float64 {
	metric, ok := metricForPhase(metrics, name, phase)
	if !ok && phase != "" {
		metric, ok = metricForPhase(metrics, name, "")
	}
	if !ok {
		return 0
	}
	return metric.Values["rate"]
}

func fillRouteMetricsPhase(metrics *WindowMetrics, summary k6Summary, phase string) {
	if hasNamedRouteMetrics(summary.Metrics, phase) {
		for index, route := range routeDefinitions {
			requestMetric, ok := summary.Metrics[fmt.Sprintf("route_requests_%s_%d", phase, index)]
			if !ok {
				continue
			}
			latency := summary.Metrics[fmt.Sprintf("route_latency_%s_%d", phase, index)]
			success := summary.Metrics[fmt.Sprintf("route_success_%s_%d", phase, index)]
			successRate := success.Values["rate"]
			metrics.Routes = append(metrics.Routes, RouteMetrics{
				Template: route.Template, Category: route.Category, Method: route.Method,
				Requests: uint64(math.Round(requestMetric.Values["count"])),
				P95MS:    latency.Values["p(95)"], P99MS: latency.Values["p(99)"],
				ErrorRate: 1 - successRate,
			})
		}
		sort.Slice(metrics.Routes, func(left, right int) bool { return metrics.Routes[left].Template < metrics.Routes[right].Template })
		return
	}
	for name, metric := range summary.Metrics {
		if !strings.HasPrefix(name, "business_requests{") {
			continue
		}
		tags := parseTags(name)
		if phase != "" && tags["phase"] != phase {
			continue
		}
		route := RouteMetrics{Template: tags["route"], Category: tags["category"], Method: tags["method"], Requests: uint64(math.Round(metric.Values["count"]))}
		for latencyName, latency := range summary.Metrics {
			if !strings.HasPrefix(latencyName, "business_latency{") {
				continue
			}
			latencyTags := parseTags(latencyName)
			if sameTags(tags, latencyTags) {
				route.P95MS = latency.Values["p(95)"]
				route.P99MS = latency.Values["p(99)"]
				break
			}
		}
		metrics.Routes = append(metrics.Routes, route)
	}
	sort.Slice(metrics.Routes, func(left, right int) bool { return metrics.Routes[left].Template < metrics.Routes[right].Template })
}

func hasNamedRouteMetrics(metrics map[string]k6Metric, phase string) bool {
	_, ok := metrics["route_requests_"+phase+"_0"]
	return ok
}

type routeDefinition struct {
	Category string
	Method   string
	Template string
}

var routeDefinitions = []routeDefinition{
	{Category: "read", Method: "GET", Template: "GET /api/v1/posts"},
	{Category: "read", Method: "GET", Template: "GET /api/v1/posts/:postId"},
	{Category: "read", Method: "GET", Template: "GET /api/v1/posts/following"},
	{Category: "read", Method: "GET", Template: "GET /api/v1/posts/:postId/comments"},
	{Category: "read", Method: "GET", Template: "GET /api/v1/users/:username"},
	{Category: "search", Method: "GET", Template: "GET /api/v1/search/posts"},
	{Category: "search", Method: "GET", Template: "GET /api/v1/search/users"},
	{Category: "notification_bookmark_read", Method: "GET", Template: "GET /api/v1/notifications"},
	{Category: "notification_bookmark_read", Method: "GET", Template: "GET /api/v1/bookmarks"},
	{Category: "notification_bookmark_read", Method: "GET", Template: "GET /api/v1/users/me/following"},
	{Category: "notification_bookmark_read", Method: "GET", Template: "GET /api/v1/users/me/followers"},
	{Category: "content_write", Method: "POST", Template: "POST /api/v1/posts"},
	{Category: "content_write", Method: "POST", Template: "POST /api/v1/posts/:postId/comments"},
	{Category: "content_write", Method: "PATCH", Template: "PATCH /api/v1/posts/:postId"},
	{Category: "content_write", Method: "DELETE", Template: "DELETE /api/v1/posts/:postId"},
	{Category: "interaction_write", Method: "PUT", Template: "PUT /api/v1/posts/:postId/like"},
	{Category: "interaction_write", Method: "DELETE", Template: "DELETE /api/v1/posts/:postId/like"},
	{Category: "interaction_write", Method: "PUT", Template: "PUT /api/v1/users/:userId/follow"},
	{Category: "interaction_write", Method: "DELETE", Template: "DELETE /api/v1/users/:userId/follow"},
	{Category: "interaction_write", Method: "PUT", Template: "PUT /api/v1/posts/:postId/bookmark"},
	{Category: "identity_session", Method: "POST", Template: "POST /api/v1/auth/login"},
	{Category: "identity_session", Method: "GET", Template: "GET /api/v1/users/me"},
}

func sameTags(left, right map[string]string) bool {
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func parseTags(metricName string) map[string]string {
	result := map[string]string{}
	start := strings.IndexByte(metricName, '{')
	if start < 0 || !strings.HasSuffix(metricName, "}") {
		return result
	}
	for _, part := range strings.Split(metricName[start+1:len(metricName)-1], ",") {
		key, value, found := strings.Cut(part, ":")
		if found {
			result[key] = value
		}
	}
	return result
}

func WriteReport(path string, report Report) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".k6-report-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
