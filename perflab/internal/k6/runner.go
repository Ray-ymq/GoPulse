package k6

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/perflab/internal/input"
)

type Runner struct {
	Root         string
	Config       Config
	ConfigPath   string
	ConfigSHA256 string
	K6Binary     string
	ReportPath   string
	Now          func() time.Time
}

func NewRunner(root, configPath string, config Config, digest string) *Runner {
	return &Runner{Root: root, Config: config, ConfigPath: configPath, ConfigSHA256: digest, Now: time.Now}
}

func (r *Runner) Run(ctx context.Context, mode Mode, boundary BoundaryRange) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.Config.Validate(r.Root); err != nil {
		return Report{}, err
	}
	if err := r.validateArtifacts(); err != nil {
		return Report{}, err
	}
	if r.K6Binary == "" {
		binary, err := ResolveK6(r.Root, r.Config.K6)
		if err != nil {
			return Report{}, err
		}
		r.K6Binary = binary
	}
	plan, err := BuildPlan(r.Config, mode, boundary)
	if err != nil {
		return Report{}, err
	}
	if err := r.healthCheck(ctx); err != nil {
		return Report{}, err
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	report := Report{
		SchemaVersion:   ReportSchemaVersion,
		Mode:            mode,
		RunID:           fmt.Sprintf("%s-%d", mode, r.Now().UnixNano()),
		ConfigPath:      r.ConfigPath,
		ConfigSHA256:    r.ConfigSHA256,
		StartedAt:       r.Now().UTC(),
		ExecutionStatus: "running",
		Thresholds:      r.Config.Thresholds,
		Stages:          make([]StageResult, 0, len(plan.Stages)),
		Resources:       ResourceMetrics{Source: "not_observed"},
	}
	reportPath, err := r.newReportPath(mode)
	if err != nil {
		return Report{}, err
	}
	r.ReportPath = reportPath

	var runErr error
	switch mode {
	case ModeRefine:
		runErr = r.runRefine(ctx, &report, boundary)
	default:
		for index, stage := range plan.Stages {
			result, stageErr := r.runStage(ctx, stage, report.RunID, index)
			report.Stages = append(report.Stages, result)
			if stageErr != nil {
				runErr = stageErr
				break
			}
			if result.Status == StatusEnvironmentFailure || result.Status == StatusIncomplete {
				runErr = fmt.Errorf("stage %s failed: %s", result.Name, result.Error)
				break
			}
			if result.Status == StatusUnstable || result.Status == StatusCapacityInsufficient {
				if mode == ModeBoundary {
					r.addUnexecuted(&report, plan.Stages[index+1:])
				}
				break
			}
			if err := r.persist(&report); err != nil {
				runErr = err
				break
			}
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		report.ExecutionStatus = "incomplete"
		if runErr == nil {
			runErr = ctx.Err()
		}
	} else if runErr != nil {
		report.ExecutionStatus = "incomplete"
	} else {
		report.ExecutionStatus = "complete"
	}
	r.ReportBoundary(&report)
	report.FinishedAt = r.Now().UTC()
	if writeErr := r.persist(&report); writeErr != nil {
		if runErr != nil {
			return report, errors.Join(runErr, writeErr)
		}
		return report, writeErr
	}
	if runErr != nil {
		return report, runErr
	}
	return report, nil
}

func (r *Runner) runRefine(ctx context.Context, report *Report, boundary BoundaryRange) error {
	for index := 0; index < r.Config.Refine.MaxIterations && boundary.UnstableRPS-boundary.StableRPS > boundary.StableRPS*r.Config.Refine.MaxRelativeWidth; index++ {
		target := roundRPS((boundary.StableRPS + boundary.UnstableRPS) / 2)
		stage := StagePlan{
			Name: fmt.Sprintf("refine-rps-%g", target), TargetRPS: target,
			MeasurementSeconds: r.Config.Refine.MeasurementSeconds, RecoverySeconds: r.Config.Refine.RecoverySeconds,
		}
		result, err := r.runStage(ctx, stage, report.RunID, index)
		report.Stages = append(report.Stages, result)
		if err != nil {
			return err
		}
		if result.Status == StatusEnvironmentFailure || result.Status == StatusIncomplete {
			return fmt.Errorf("stage %s failed: %s", result.Name, result.Error)
		}
		boundary = nextRefineInterval(boundary, target, result.Status == StatusStable)
		if err := r.persist(report); err != nil {
			return err
		}
		if result.Status != StatusStable && result.Status != StatusUnstable {
			return nil
		}
	}
	return nil
}

func (r *Runner) runStage(ctx context.Context, stage StagePlan, runID string, index int) (StageResult, error) {
	result := StageResult{Name: stage.Name, TargetRPS: stage.TargetRPS, Status: StatusEnvironmentFailure}
	baseline, baselineStatus, baselineErr := r.backlog(ctx)
	if baselineErr != nil {
		result.Error = baselineErr.Error()
		result.Classification = []string{"backlog_probe_failed"}
		return result, nil
	}
	result.Recovery = RecoveryResult{Status: baselineStatus, Baseline: baseline}
	summaryPath := filepath.Join(r.Root, r.Config.ReportDir, fmt.Sprintf("%s-%02d-summary.json", sanitizeName(stage.Name), index+1))
	exitCode, runErr := r.runK6(ctx, stage, summaryPath, runID)
	result.K6ExitCode = exitCode
	result.K6SummaryPath = summaryPath
	if runErr != nil && !fileExists(summaryPath) {
		result.Error = runErr.Error()
		result.Classification = []string{"k6_execution_failed"}
		return result, nil
	}
	measurement, summaryErr := ReadSummaryPhase(summaryPath, "measurement", stage.TargetRPS, stage.MeasurementSeconds)
	if summaryErr != nil {
		result.Status = StatusEnvironmentFailure
		result.Error = summaryErr.Error()
		result.Classification = []string{"k6_summary_invalid"}
		return result, nil
	}
	if stage.WarmupSeconds > 0 {
		result.Warmup, _ = ReadSummaryPhase(summaryPath, "warmup", stage.TargetRPS, stage.WarmupSeconds)
	}
	result.Measurement = measurement
	if runErr != nil && measurement.Requests == 0 {
		result.Status = StatusEnvironmentFailure
		result.Error = runErr.Error()
		result.Classification = []string{"k6_execution_failed"}
		return result, nil
	}
	recovery := r.waitForRecovery(ctx, baselineStatus, baseline, stage.RecoverySeconds)
	result.Recovery = recovery
	result.Status, result.Classification = classify(measurement, recovery, r.Config.Thresholds, r.Config.Recovery.RequireObservation)
	if runErr != nil {
		result.Classification = append(result.Classification, "k6_threshold_abort")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		result.Status = StatusIncomplete
		result.Classification = append(result.Classification, "interrupted")
	}
	return result, nil
}

func (r *Runner) runK6(ctx context.Context, stage StagePlan, summaryPath, runID string) (int, error) {
	if err := os.MkdirAll(filepath.Dir(summaryPath), 0o700); err != nil {
		return -1, err
	}
	command := exec.CommandContext(ctx, r.K6Binary, "run", "--summary-export", summaryPath, ResolvePath(r.Root, r.Config.K6.Script))
	environment := append([]string{}, os.Environ()...)
	values := map[string]string{
		"GOPULSE_BASE_URL":             r.Config.Target.BaseURL,
		"LOAD_CORPUS_PATH":             ResolvePath(r.Root, r.Config.Target.CorpusPath),
		"LOAD_CREDENTIALS_PATH":        ResolvePath(r.Root, r.Config.Target.CredentialsPath),
		"LOAD_COOKIE_NAME":             r.Config.Target.CookieName,
		"LOAD_MIX_JSON":                mustJSON(r.Config.Workload.Mix),
		"LOAD_VUS":                     strconv.Itoa(r.Config.Workload.VirtualUsers),
		"LOAD_REQUEST_TIMEOUT_SECONDS": strconv.FormatFloat(r.Config.Workload.RequestTimeoutSeconds, 'f', -1, 64),
		"LOAD_TARGET_RPS":              strconv.FormatFloat(stage.TargetRPS, 'f', -1, 64),
		"LOAD_WARMUP_SECONDS":          strconv.FormatFloat(stage.WarmupSeconds, 'f', -1, 64),
		"LOAD_MEASUREMENT_SECONDS":     strconv.FormatFloat(stage.MeasurementSeconds, 'f', -1, 64),
		"LOAD_HEALTH_URL":              r.Config.Target.HealthURL,
		"LOAD_MAX_ERROR_RATE":          strconv.FormatFloat(r.Config.Thresholds.MaxErrorRate, 'f', -1, 64),
		"LOAD_EARLY_STOP_ERROR_RATE":   strconv.FormatFloat(r.Config.Thresholds.EarlyStopErrorRate, 'f', -1, 64),
		"LOAD_EARLY_STOP_SECONDS":      strconv.FormatFloat(r.Config.Thresholds.EarlyStopSeconds, 'f', -1, 64),
		"LOAD_HEALTH_FAILURE_SECONDS":  strconv.FormatFloat(r.Config.Thresholds.HealthFailureSeconds, 'f', -1, 64),
		"LOAD_STAGE_RUN_ID":            runID,
	}
	for key, value := range values {
		environment = append(environment, key+"="+value)
	}
	command.Env = environment
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err := command.Run()
	if err == nil {
		return 0, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), err
	}
	return -1, err
}

func (r *Runner) healthCheck(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Config.Target.HealthURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 4096)); err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("health check returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (r *Runner) backlog(ctx context.Context) (float64, string, error) {
	if r.Config.Target.BacklogURL == "" {
		return 0, "not_observed", nil
	}
	value, err := r.probeBacklog(ctx)
	if err != nil {
		return 0, "probe_failed", err
	}
	return value, "observed", nil
}

func (r *Runner) probeBacklog(ctx context.Context) (float64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.Config.Target.BacklogURL, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("backlog probe returned HTTP %d", response.StatusCode)
	}
	var payload any
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&payload); err != nil {
		return 0, err
	}
	value, err := jsonPathNumber(payload, r.Config.Target.BacklogPath)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func (r *Runner) waitForRecovery(ctx context.Context, status string, baseline float64, seconds float64) RecoveryResult {
	result := RecoveryResult{Status: status, Baseline: baseline}
	if status == "not_observed" {
		return result
	}
	if status != "observed" || seconds <= 0 {
		result.Status = "probe_failed"
		return result
	}
	started := time.Now()
	for {
		value, err := r.probeBacklog(ctx)
		result.After = value
		result.ElapsedSeconds = time.Since(started).Seconds()
		if err == nil && value <= baseline+r.Config.Recovery.BaselineTolerance {
			result.Status = "recovered"
			result.Recovered = true
			return result
		}
		if result.ElapsedSeconds >= seconds {
			result.Status = "not_recovered"
			if err != nil {
				result.Error = err.Error()
			}
			return result
		}
		select {
		case <-ctx.Done():
			result.Status = "interrupted"
			result.Error = ctx.Err().Error()
			return result
		case <-time.After(time.Duration(r.Config.Recovery.PollSeconds * float64(time.Second))):
		}
	}
}

func (r *Runner) validateArtifacts() error {
	if _, err := input.LoadCorpus(ResolvePath(r.Root, r.Config.Target.CorpusPath)); err != nil {
		return err
	}
	if _, err := input.LoadCredentials(ResolvePath(r.Root, r.Config.Target.CredentialsPath)); err != nil {
		return err
	}
	return nil
}

func (r *Runner) newReportPath(mode Mode) (string, error) {
	directory := ResolvePath(r.Root, r.Config.ReportDir)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(directory, fmt.Sprintf("%s-%s.json", mode, r.Now().UTC().Format("20060102T150405.000000000Z"))), nil
}

func (r *Runner) persist(report *Report) error {
	r.ReportBoundary(report)
	return WriteReport(r.ReportPath, *report)
}

func (r *Runner) ReportBoundary(report *Report) {
	boundary := boundaryFromResults(report.Stages)
	report.Boundary = BoundarySummary{
		StableRPS: boundary.StableRPS, FirstUnstableRPS: boundary.UnstableRPS,
		BoundaryFound:     boundary.Found,
		CapacityJudgement: map[bool]string{true: "confirmed_interval", false: "boundary_not_found"}[boundary.Found],
	}
	if report.Mode == ModeSoak && len(report.Stages) > 0 && report.Stages[0].Status == StatusStable {
		report.Boundary.StableRPS = report.Stages[0].TargetRPS / r.Config.Soak.StableCapacityRatio
		report.Boundary.CapacityJudgement = "soak_stable_at_configured_ratio"
	}
}

func (r *Runner) addUnexecuted(report *Report, stages []StagePlan) {
	for _, stage := range stages {
		report.Stages = append(report.Stages, StageResult{Name: stage.Name, TargetRPS: stage.TargetRPS, Status: StatusNotExecuted, Classification: []string{"stopped_after_first_unstable_stage"}})
	}
}

func LoadPreviousBoundary(root string, config Config, explicit string) (BoundaryRange, error) {
	path := explicit
	if path == "" {
		path = config.PreviousReport
	}
	if path == "" {
		return BoundaryRange{}, errors.New("refine and soak require previous_report or --previous-report")
	}
	encoded, err := os.ReadFile(ResolvePath(root, path))
	if err != nil {
		return BoundaryRange{}, fmt.Errorf("read previous load report: %w", err)
	}
	var report Report
	if err := json.Unmarshal(encoded, &report); err != nil {
		return BoundaryRange{}, fmt.Errorf("decode previous load report: %w", err)
	}
	boundary := BoundaryRange{StableRPS: report.Boundary.StableRPS, UnstableRPS: report.Boundary.FirstUnstableRPS, Found: report.Boundary.BoundaryFound}
	if report.Mode == ModeRefine {
		if calculated := boundaryFromResults(report.Stages); calculated.StableRPS > 0 {
			boundary.StableRPS = calculated.StableRPS
		}
	}
	if report.Mode == ModeSoak && boundary.StableRPS > 0 {
		boundary.Found = true
	}
	return boundary, nil
}

func jsonPathNumber(value any, path string) (float64, error) {
	if path == "" {
		if number, ok := value.(float64); ok {
			return number, nil
		}
	}
	current := value
	for _, key := range strings.Split(strings.TrimPrefix(path, "."), ".") {
		if key == "" {
			continue
		}
		object, ok := current.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("backlog JSON path %q is not an object", path)
		}
		current, ok = object[key]
		if !ok {
			return 0, fmt.Errorf("backlog JSON path %q is missing", path)
		}
	}
	number, ok := current.(float64)
	if !ok {
		return 0, fmt.Errorf("backlog JSON path %q is not numeric", path)
	}
	return number, nil
}

func mustJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func roundRPS(value float64) float64 {
	return float64(int64(value*100+0.5)) / 100
}

func sanitizeName(value string) string {
	value = strings.ReplaceAll(value, ".", "_")
	value = strings.ReplaceAll(value, "/", "_")
	return value
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
