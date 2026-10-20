package k6

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

type safetyStatus struct {
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
}

func checkSafety(path string, now time.Time) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.New("安全观测状态不可读")
	}
	var status safetyStatus
	if json.Unmarshal(data, &status) != nil || status.Timestamp.IsZero() {
		return errors.New("安全观测状态无效")
	}
	if now.Sub(status.Timestamp) > 3*time.Second || now.Before(status.Timestamp.Add(-time.Second)) {
		return errors.New("安全观测已中断")
	}
	if status.Status != "ready" {
		return fmt.Errorf("安全观测要求停压: %s", status.Reason)
	}
	return nil
}

// runContinuous 用同一个 k6 场景执行全部档位，避免档间登录或恢复等待。
func (r *Runner) runContinuous(ctx context.Context, mode Mode, boundary BoundaryRange) (Report, error) {
	if mode != ModeBoundary {
		return Report{}, errors.New("独立复测须恢复快照后以单档 boundary 配置执行")
	}
	if r.Config.SafetyFile == "" {
		return Report{}, errors.New("连续加压必须设置 safety_file")
	}
	safety := ResolvePath(r.Root, r.Config.SafetyFile)
	if err := checkSafety(safety, time.Now()); err != nil {
		return Report{}, err
	}
	if err := r.healthCheck(ctx); err != nil {
		return Report{}, err
	}
	plan, err := BuildPlan(r.Config, mode, boundary)
	if err != nil {
		return Report{}, err
	}
	dir := ResolvePath(r.Root, r.Config.ReportDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Report{}, err
	}
	runID := "continuous-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	summary := filepath.Join(dir, runID+"-summary.json")
	samples := filepath.Join(dir, runID+"-samples.json")
	r.ReportPath = filepath.Join(dir, runID+"-report.json")
	stages, _ := json.Marshal(plan.Stages)
	command := exec.Command(r.K6Binary, "run", "--out", "json="+samples, ResolvePath(r.Root, r.Config.K6.Script))
	command.Env = append(os.Environ(),
		"GOPULSE_BASE_URL="+r.Config.Target.BaseURL,
		"LOAD_HEALTH_URL="+r.Config.Target.HealthURL,
		"LOAD_CORPUS_PATH="+ResolvePath(r.Root, r.Config.Target.CorpusPath),
		"LOAD_CREDENTIALS_PATH="+ResolvePath(r.Root, r.Config.Target.CredentialsPath),
		"LOAD_COOKIE_NAME="+r.Config.Target.CookieName,
		"LOAD_STAGES_JSON="+string(stages),
		"LOAD_SUMMARY_PATH="+summary,
		"LOAD_MIX_JSON="+mustJSON(r.Config.Workload.Mix),
		"LOAD_PROFILE="+r.Config.Workload.Profile,
		"LOAD_VUS="+strconv.Itoa(r.Config.Workload.VirtualUsers),
		"LOAD_REQUEST_TIMEOUT_SECONDS="+strconv.FormatFloat(r.Config.Workload.RequestTimeoutSeconds, 'f', -1, 64))
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	report := Report{SchemaVersion: ReportSchemaVersion, Mode: mode, RunID: runID, ConfigPath: r.ConfigPath, ConfigSHA256: r.ConfigSHA256, StartedAt: time.Now().UTC(), Thresholds: r.Config.Thresholds, ExecutionStatus: "running", Resources: ResourceMetrics{Source: "external_guard"}}
	if err := command.Start(); err != nil {
		return report, err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	var stopErr, runErr error
	var stoppedAt time.Time
	running := true
	for running {
		select {
		case runErr = <-done:
			running = false
		case <-ctx.Done():
			stopErr = ctx.Err()
			running = false
		case <-tick.C:
			if err := checkSafety(safety, time.Now()); err != nil {
				stopErr = err
				running = false
			}
		}
	}
	if stopErr != nil {
		stoppedAt = time.Now()
		_ = command.Process.Signal(os.Interrupt)
		select {
		case runErr = <-done:
		case <-time.After(8 * time.Second):
			_ = command.Process.Kill()
			runErr = <-done
		}
	}
	if !stoppedAt.IsZero() {
		_ = correctStoppedWindows(summary, stoppedAt, plan.Stages, true)
	}

	if err := addDroppedWindows(samples, summary, plan.Stages); err != nil && runErr == nil {
		runErr = err
	}
	for index, stage := range plan.Stages {
		phase := fmt.Sprintf("stage%d_measurement", index)
		measurement, parseErr := ReadSummaryPhase(summary, phase, stage.TargetRPS, 0)
		measurement.HealthFailureRate = 0
		warmup, _ := ReadSummaryPhase(summary, fmt.Sprintf("stage%d_warmup", index), stage.TargetRPS, 0)
		result := StageResult{Name: stage.Name, TargetRPS: stage.TargetRPS, Measurement: measurement, Warmup: warmup, K6SummaryPath: summary, Recovery: RecoveryResult{Status: "group_observation_required"}}
		switch {
		case parseErr != nil:
			result.Status = StatusIncomplete
			result.Error = parseErr.Error()
		case measurement.ActualSeconds == 0:
			result.Status = StatusNotExecuted
		case measurement.ActualSeconds < stage.MeasurementSeconds:
			result.Status = StatusIncomplete
		default:
			result.Status, result.Classification = classify(measurement, RecoveryResult{}, r.Config.Thresholds, false)
			if measurement.ActualRequestRate < stage.TargetRPS*r.Config.Thresholds.MinAchievedRPSRatio {
				result.Status = StatusCapacityInsufficient
			}
		}
		report.Stages = append(report.Stages, result)
	}
	report.FinishedAt = time.Now().UTC()
	report.ExecutionStatus = "complete"
	if stopErr != nil {
		report.StopReason = stopErr.Error()
	}
	if runErr != nil || stopErr != nil {
		report.ExecutionStatus = "incomplete"
	}
	r.ReportBoundary(&report)
	if err := r.persist(&report); err != nil {
		return report, err
	}
	return report, errors.Join(stopErr, runErr)
}

// 停压时冻结发压窗口，排除请求收尾时间。
func correctStoppedWindows(path string, stoppedAt time.Time, stages []StagePlan, continuous bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var summary struct {
		Metrics map[string]map[string]float64 `json:"metrics"`
		Timing  map[string]any                `json:"timing"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		return err
	}
	start := summary.Metrics["scenario_started_at_ms"]["min"]
	if start <= 0 {
		return errors.New("缺少场景开始时间，无法计算提前停压窗口")
	}
	elapsed := float64(stoppedAt.UnixMilli())/1000 - start/1000
	if elapsed < 0 {
		elapsed = 0
	}
	offset := 0.0
	for index, stage := range stages {
		for _, phase := range []string{"warmup", "measurement"} {
			planned := stage.WarmupSeconds
			if phase == "measurement" {
				planned = stage.MeasurementSeconds
			}
			seconds := max(0, min(planned, elapsed-offset))
			suffix := phase
			if continuous {
				suffix = fmt.Sprintf("stage%d_%s", index, phase)
			}
			summary.Metrics["window_seconds_"+suffix] = map[string]float64{"value": seconds}
			offset += planned
		}
	}
	summary.Timing["stop_requested_at"] = stoppedAt.UTC().Format(time.RFC3339Nano)
	summary.Timing["actual_load_seconds"] = elapsed
	data, err = json.Marshal(summary)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func addDroppedWindows(samplesPath, summaryPath string, stages []StagePlan) error {
	encoded, err := os.ReadFile(summaryPath)
	if err != nil {
		return err
	}
	var summary map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &summary); err != nil {
		return err
	}
	var metrics map[string]map[string]float64
	if err := json.Unmarshal(summary["metrics"], &metrics); err != nil {
		return err
	}
	start := metrics["scenario_started_at_ms"]["min"]
	if start <= 0 {
		return nil
	}
	file, err := os.Open(samplesPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte("dropped_iterations")) {
			continue
		}
		var point struct {
			Type   string `json:"type"`
			Metric string `json:"metric"`
			Data   struct {
				Time  time.Time         `json:"time"`
				Value float64           `json:"value"`
				Tags  map[string]string `json:"tags"`
			} `json:"data"`
		}
		if json.Unmarshal(line, &point) != nil || point.Type != "Point" || point.Metric != "dropped_iterations" || point.Data.Tags["scenario"] != "continuous" {
			continue
		}
		elapsed := float64(point.Data.Time.UnixMilli())/1000 - start/1000
		offset := 0.0
		for index, stage := range stages {
			end := offset + stage.WarmupSeconds + stage.MeasurementSeconds
			if elapsed >= offset && elapsed < end {
				phase := "measurement"
				if elapsed < offset+stage.WarmupSeconds {
					phase = "warmup"
				}
				name := fmt.Sprintf("dropped_iterations_stage%d_%s", index, phase)
				if metrics[name] == nil {
					metrics[name] = map[string]float64{}
				}
				metrics[name]["count"] += point.Data.Value
				break
			}
			offset = end
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	summary["metrics"], err = json.Marshal(metrics)
	if err != nil {
		return err
	}
	encoded, err = json.Marshal(summary)
	if err != nil {
		return err
	}
	return os.WriteFile(summaryPath, encoded, 0600)
}
