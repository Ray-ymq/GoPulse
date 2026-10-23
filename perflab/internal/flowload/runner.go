package flowload

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Safety struct {
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	Backlog   float64   `json:"backlog"`
}

func safety(path string) (Safety, error) {
	var s Safety
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Status != "ready" || time.Since(s.Timestamp) > 12*time.Second || time.Until(s.Timestamp) > time.Second {
		return s, fmt.Errorf("观测或保护条件无效：%s", s.Reason)
	}
	return s, nil
}

func ownedStop(t Task, name string) {
	// 先核对运行标签，再按容器 ID 停止，避免误停同名容器。
	inspectContext, stopInspect := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopInspect()
	b, err := exec.CommandContext(inspectContext, "docker", "inspect", name).Output()
	if err != nil {
		return
	}
	var rows []struct {
		ID     string `json:"Id"`
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if json.Unmarshal(b, &rows) != nil || len(rows) != 1 || rows[0].Config.Labels["io.gopulse.pt.run"] != t.RunID {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "docker", "stop", "--timeout", "5", rows[0].ID).Run()
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", rows[0].ID).Run()
}

func runStage(ctx context.Context, t Task, stage Stage, index int, archive *os.File) (Result, int64, error) {
	dir := filepath.Join(t.Output, fmt.Sprintf("stage-%02d", index))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Result{}, t.Offset, err
	}
	if _, err := safety(t.SafetyFile); err != nil {
		return Result{}, t.Offset, err
	}
	name := t.Project + "-load-" + t.RunID + "-" + strconv.Itoa(index)
	args := []string{"compose", "--project-name", t.Project, "--env-file", filepath.Join(t.Preparation, "test.env"), "-f", filepath.Join(t.Root, "deploy/compose.business.yaml"), "-f", filepath.Join(t.Preparation, "resources.json"), "run", "--rm", "--no-deps", "--name", name, "--label", "io.gopulse.pt.run=" + t.RunID, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "-v", t.Preparation + ":/input:ro", "-v", filepath.Join(t.Root, "perflab") + ":/perflab:ro", "-v", dir + ":/output"}
	values := map[string]string{"GOPULSE_BASE_URL": "http://frontend:8080", "LOAD_TARGET_RPS": strconv.Itoa(stage.Target), "LOAD_WARMUP_SECONDS": strconv.Itoa(stage.Warmup), "LOAD_MEASUREMENT_SECONDS": strconv.Itoa(stage.Measurement), "LOAD_SLOT_OFFSET": strconv.FormatInt(t.Offset, 10), "LOAD_MIX_JSON": string(t.Mix), "LOAD_ROUTE": strconv.Itoa(t.Route), "LOAD_STAGE_ID": t.GroupID + "-" + strconv.Itoa(index)}
	for k, v := range values {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, "k6", "run", "--quiet", "--log-format", "raw", "/perflab/flow/workload.js")
	command := exec.Command("docker", args...)
	// Compose 只使用私有 env 文件，不让宿主机同名变量覆盖固定配置。
	raw, err := os.ReadFile(filepath.Join(t.Preparation, "test.env"))
	if err != nil {
		return Result{}, t.Offset, err
	}
	defined := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if k, _, ok := strings.Cut(line, "="); ok {
			defined[k] = true
		}
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !defined[key] {
			command.Env = append(command.Env, entry)
		}
	}
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	pipeR, pipeW := io.Pipe()
	command.Stdout = pipeW
	command.Stderr = pipeW
	log, err := os.OpenFile(filepath.Join(dir, "k6.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Result{}, t.Offset, err
	}
	defer func() { _ = log.Close() }()
	if err = command.Start(); err != nil {
		return Result{}, t.Offset, err
	}
	defer ownedStop(t, name)
	accumulator := Accumulator{Stage: stage}
	starts := map[string]Event{}
	lines := make(chan string, 256)
	sourceLines := lines
	scanErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(pipeR)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		scanErr <- scanner.Err()
		close(lines)
	}()
	done := make(chan error, 1)
	go func() { e := command.Wait(); _ = pipeW.Close(); done <- e }()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	scenario := 0.0
	reason := ""
	stopped := false
	ended := 0.0
	lastCompletion := time.Now()
	begin := time.Now()
	windows := map[string]*Window{}
	requestStop := func(why string) {
		if !stopped {
			reason = why
			stopped = true
			ended = float64(time.Now().UnixMilli())
			accumulator.End = ended
			ownedStop(t, name)
		}
	}
	var runErr error
	running := true
	for running {
		select {
		case line, ok := <-lines:
			if !ok {
				lines = nil
				continue
			}
			_, _ = fmt.Fprintln(log, line)
			pos := strings.Index(line, "PT {")
			if pos < 0 {
				continue
			}
			var event Event
			if json.Unmarshal([]byte(line[pos+3:]), &event) != nil || event.SchemaVersion != EventVersion {
				requestStop("invalid_events")
				continue
			}
			if event.Type == "fatal" {
				requestStop(event.Error)
				continue
			}
			if event.Type != "start" && event.Type != "outcome" {
				requestStop("invalid_events")
				continue
			}
			if event.RequestID == "" || event.Route == "" || event.Started <= 0 {
				requestStop("invalid_events")
				continue
			}
			if scenario == 0 {
				scenario = event.ScenarioStarted
				accumulator.Scenario = scenario
				windows["warmup"] = &Window{Start: scenario}
				windows["measurement"] = &Window{Start: scenario + float64(stage.Warmup)*1000}
			}
			if event.Type == "start" {
				starts[event.RequestID] = event
			}
			if event.Type == "outcome" {
				if _, ok := starts[event.RequestID]; !ok {
					requestStop("orphan_outcome")
				}
				delete(starts, event.RequestID)
				lastCompletion = time.Now()
				phase := "warmup"
				if event.Completed >= scenario+float64(stage.Warmup)*1000 {
					phase = "measurement"
				}
				if event.Completed >= scenario+float64(stage.Warmup+stage.Measurement)*1000 {
					phase = "drain"
				}
				event.CompletionPhase = phase
				windowEvent := Event{Type: event.Type, Completed: event.Completed, Success: event.Success}
				if window := windows[phase]; window != nil {
					window.Events = append(window.Events, windowEvent)
				}
			}
			b, _ := json.Marshal(event)
			accumulator.Add(event)
			_, _ = archive.Write(append(b, '\n'))
		case runErr = <-done:
			running = false
		case <-ctx.Done():
			requestStop("manual_stop")
		case <-tick.C:
			s, e := safety(t.SafetyFile)
			if e != nil {
				requestStop("environment_or_observation: " + e.Error())
			}
			now := float64(time.Now().UnixMilli())
			phase := "warmup"
			if scenario > 0 && now >= scenario+float64(stage.Warmup)*1000 {
				phase = "measurement"
			}
			var rate *float64
			if w := windows[phase]; w != nil && now < scenario+float64(stage.Warmup+stage.Measurement)*1000 {
				stop, r := w.Stop(now)
				rate = r
				if stop {
					requestStop("failure_rate_over_50_percent")
				}
			}
			status := map[string]any{"stage": stage.Name, "target_rps": stage.Target, "phase": phase, "failure_rate_10s": rate, "backlog": s.Backlog, "updated_at": time.Now().UTC()}
			if b, e := json.Marshal(status); e == nil {
				_ = os.WriteFile(filepath.Join(t.Output, "load-state.json.tmp"), b, 0600)
				_ = os.Rename(filepath.Join(t.Output, "load-state.json.tmp"), filepath.Join(t.Output, "load-state.json"))
			}
			if scenario > 0 && time.Since(lastCompletion) > 15*time.Second {
				requestStop("load_generator_no_completions")
			}
			if time.Since(begin) > time.Duration(330+stage.Warmup+stage.Measurement)*time.Second {
				requestStop("load_generator_timeout")
			}
		}
	}
	// 回收最后已经写出的事件；管道不能丢失末次完成事件。
	for line := range sourceLines {
		_, _ = fmt.Fprintln(log, line)
		pos := strings.Index(line, "PT {")
		if pos < 0 {
			continue
		}
		var e Event
		if json.Unmarshal([]byte(line[pos+3:]), &e) != nil {
			reason = "invalid_events"
			continue
		}
		if e.Type == "fatal" {
			reason = e.Error
			continue
		}
		if e.Type == "start" {
			starts[e.RequestID] = e
			if scenario == 0 {
				scenario = e.ScenarioStarted
				accumulator.Scenario = scenario
			}
			accumulator.Add(e)
			b, _ := json.Marshal(e)
			_, _ = archive.Write(append(b, '\n'))
		}
		if e.Type == "outcome" {
			delete(starts, e.RequestID)
			accumulator.Add(e)
			b, _ := json.Marshal(e)
			_, _ = archive.Write(append(b, '\n'))
		}
	}
	if e := <-scanErr; e != nil {
		reason = "event_stream_failed"
	}
	if ended == 0 {
		ended = float64(time.Now().UnixMilli())
	}
	for _, e := range starts {
		e.Type = "cancel"
		e.Error = "stop_cancellation"
		e.Completed = float64(time.Now().UnixMilli())
		accumulator.Add(e)
		b, _ := json.Marshal(e)
		_, _ = archive.Write(append(b, '\n'))
	}
	code := 0
	if runErr != nil {
		code = 1
	}
	var summary struct {
		Metrics map[string]struct {
			Values map[string]float64 `json:"values"`
		} `json:"metrics"`
	}
	b, readErr := os.ReadFile(filepath.Join(dir, "summary.json"))
	if readErr == nil {
		readErr = json.Unmarshal(b, &summary)
	}
	dropped := int(summary.Metrics["dropped_iterations"].Values["count"])
	result := accumulator.Result(ended, reason, code, dropped)
	if readErr != nil && reason == "" {
		result.StopReason = "summary_missing"
		result.ExecutionCompleted = false
		result.BusinessPassed = false
	}
	nextOffset := t.Offset + int64(stage.Target*(stage.Warmup+stage.Measurement))
	if result.StopReason != "" && result.StopReason != "failure_rate_over_50_percent" {
		return result, nextOffset, fmt.Errorf("%s", result.StopReason)
	}
	if runErr != nil && reason == "" {
		return result, nextOffset, runErr
	}
	return result, nextOffset, nil
}

type Report struct {
	SchemaVersion string   `json:"schema_version"`
	Stages        []Result `json:"stages"`
	StopReason    string   `json:"stop_reason"`
	Offset        int64    `json:"offset"`
}

func Run(ctx context.Context, t Task) (Report, error) {
	report := Report{SchemaVersion: "gopulse.pt.load.v1", Offset: t.Offset}
	if err := t.Validate(); err != nil {
		return report, err
	}
	stages := t.Stages
	if t.Scan {
		var err error
		warmup, measure := 30, 300
		if len(t.Stages) > 0 {
			warmup = t.Stages[0].Warmup
			measure = t.Stages[0].Measurement
		}
		stages, err = Scan(t.StartRPS, t.MaxRPS, warmup, measure)
		if err != nil {
			return report, err
		}
	}
	if err := os.MkdirAll(t.Output, 0700); err != nil {
		return report, err
	}
	archive, err := os.OpenFile(filepath.Join(t.Output, "requests.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return report, err
	}
	defer func() { _ = archive.Close() }()
	var runErr error
	for index, stage := range stages {
		result, offset, e := runStage(ctx, t, stage, index, archive)
		t.Offset = offset
		report.Offset = offset
		report.Stages = append(report.Stages, result)
		if e != nil {
			if result.StopReason == "" {
				result.StopReason = e.Error()
				report.Stages[len(report.Stages)-1] = result
			}
			report.StopReason = result.StopReason
			runErr = e
			break
		}
		if result.StopReason != "" {
			report.StopReason = result.StopReason
			break
		}
		s, e := safety(t.SafetyFile)
		if e != nil {
			runErr = e
			break
		}
		report.Stages[len(report.Stages)-1].Backlog = s.Backlog
		if t.Scan && Plateau(report.Stages) {
			report.StopReason = "throughput_plateau"
			extra := stage
			extra.Name = "plateau-observation"
			extra.Warmup = 0
			extra.Measurement = stage.Measurement
			r, o, e := runStage(ctx, t, extra, index+1, archive)
			report.Stages = append(report.Stages, r)
			report.Offset = o
			runErr = e
			break
		}
	}
	if t.Scan && report.StopReason == "" && runErr == nil {
		report.StopReason = "scan_limit"
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(t.Output, "load-result.json"), data, 0600)
	}
	return report, errors.Join(runErr, err)
}
