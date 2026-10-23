package flowload

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

const TaskVersion = "gopulse.pt.task.v1"
const EventVersion = "gopulse.pt.request.v1"

type Task struct {
	SchemaVersion   string          `json:"schema_version"`
	GroupID         string          `json:"group_id"`
	RunID           string          `json:"run_id"`
	StartRPS        int             `json:"start_rps"`
	MaxRPS          int             `json:"max_rps"`
	Stages          []Stage         `json:"stages"`
	Output          string          `json:"output"`
	CredentialsFile string          `json:"credentials_file"`
	Preparation     string          `json:"preparation"`
	Root            string          `json:"root"`
	Project         string          `json:"project"`
	SafetyFile      string          `json:"safety_file"`
	Route           int             `json:"route"`
	Scan            bool            `json:"scan"`
	Offset          int64           `json:"offset"`
	VUs             int             `json:"vus"`
	Mix             json.RawMessage `json:"mix"`
}

type Event struct {
	SchemaVersion   string          `json:"schema_version"`
	Type            string          `json:"type"`
	RequestID       string          `json:"request_id"`
	Route           string          `json:"route"`
	Category        string          `json:"category"`
	Phase           string          `json:"phase"`
	CompletionPhase string          `json:"completion_phase,omitempty"`
	Started         float64         `json:"started_at_ms"`
	Completed       float64         `json:"completed_at_ms,omitempty"`
	ScenarioStarted float64         `json:"scenario_started_at_ms,omitempty"`
	Success         bool            `json:"success"`
	Error           string          `json:"error,omitempty"`
	HTTPMS          float64         `json:"http_ms,omitempty"`
	Status          int             `json:"status,omitempty"`
	Sample          json.RawMessage `json:"sample,omitempty"`
}

type Window struct {
	Start  float64
	Events []Event
}

// Stop 按完成时间计算最近十秒窗口；未满窗口、无完成请求和取消不报告零失败率。
func (w *Window) Stop(now float64) (bool, *float64) {
	if now-w.Start < 10000 {
		return false, nil
	}
	failures, total := 0, 0
	retained := w.Events[:0]
	for _, e := range w.Events {
		if e.Type == "outcome" && e.Phase != "setup" && e.Completed > now-10000 && e.Completed <= now {
			retained = append(retained, e)
			total++
			if !e.Success {
				failures++
			}
		}
	}
	w.Events = retained
	if total == 0 {
		return false, nil
	}
	rate := float64(failures) / float64(total)
	return rate > .5, &rate
}

type Result struct {
	Scenario           float64     `json:"scenario_started_ms"`
	ObservedEnd        float64     `json:"observed_end_ms"`
	Backlog            float64     `json:"backlog"`
	Name               string      `json:"name"`
	Target             int         `json:"target_rps"`
	BusinessPassed     bool        `json:"business_passed"`
	ExecutionCompleted bool        `json:"execution_completed"`
	StopReason         string      `json:"stop_reason,omitempty"`
	ActualRPS          float64     `json:"actual_rps"`
	SuccessRPS         float64     `json:"success_rps"`
	FailureRate        *float64    `json:"failure_rate"`
	P95                float64     `json:"p95_ms"`
	P99                float64     `json:"p99_ms"`
	Started            int         `json:"started"`
	Completed          int         `json:"completed"`
	Canceled           int         `json:"canceled"`
	Dropped            int         `json:"dropped_iterations"`
	Begin              float64     `json:"begin_ms"`
	End                float64     `json:"end_ms"`
	Setup              *SetupStats `json:"setup,omitempty"`
}

type SetupStats struct {
	Started   int `json:"started"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Canceled  int `json:"canceled"`
}

type Accumulator struct {
	Stage     Stage
	Scenario  float64
	End       float64
	Started   int
	Canceled  int
	Success   int
	Latencies []float64
	Setup     SetupStats
}

func (a *Accumulator) Add(e Event) {
	if e.Phase == "setup" {
		switch e.Type {
		case "start":
			a.Setup.Started++
		case "cancel":
			a.Setup.Canceled++
		case "outcome":
			a.Setup.Completed++
			if !e.Success {
				a.Setup.Failed++
			}
		}
		return
	}
	if e.Type == "start" {
		a.Started++
		return
	}
	if e.Type == "cancel" {
		a.Canceled++
		return
	}
	begin := a.Scenario + float64(a.Stage.Warmup)*1000
	end := begin + float64(a.Stage.Measurement)*1000
	if a.End > 0 {
		end = math.Min(end, a.End)
	}
	if e.Type == "outcome" && e.Completed >= begin && e.Completed < end {
		a.Latencies = append(a.Latencies, e.HTTPMS)
		if e.Success {
			a.Success++
		}
	}
}

func (a *Accumulator) Result(end float64, reason string, code, dropped int) Result {
	stage := a.Stage
	r := Result{Name: stage.Name, Target: stage.Target, StopReason: reason, Dropped: dropped, Begin: a.Scenario + float64(stage.Warmup)*1000,
		Started: a.Started, Canceled: a.Canceled, Completed: len(a.Latencies), Scenario: a.Scenario, ObservedEnd: math.Min(end, a.Scenario+float64(stage.Warmup+stage.Measurement)*1000)}
	r.End = math.Min(end, r.Begin+float64(stage.Measurement)*1000)
	if a.Setup.Started > 0 {
		value := a.Setup
		r.Setup = &value
	}
	seconds := math.Max(0, (r.End-r.Begin)/1000)
	if seconds > 0 {
		r.ActualRPS = float64(r.Completed) / seconds
		r.SuccessRPS = float64(a.Success) / seconds
	}
	if r.Completed > 0 {
		rate := float64(r.Completed-a.Success) / float64(r.Completed)
		r.FailureRate = &rate
	}
	sort.Float64s(a.Latencies)
	quantile := func(p float64) float64 {
		if len(a.Latencies) == 0 {
			return 0
		}
		return a.Latencies[int(math.Ceil(p*float64(len(a.Latencies))))-1]
	}
	r.P95, r.P99 = quantile(.95), quantile(.99)
	r.ExecutionCompleted = reason == "" && code == 0 && a.Scenario > 0 && seconds >= float64(stage.Measurement)-.01
	r.BusinessPassed = r.ExecutionCompleted && r.FailureRate != nil && *r.FailureRate <= .01 && r.P95 <= 1000 && r.P99 <= 2000 && r.ActualRPS >= float64(stage.Target)*.95
	return r
}

func Evaluate(stage Stage, events []Event, scenario, end float64, reason string, code, dropped int) Result {
	accumulator := Accumulator{Stage: stage, Scenario: scenario, End: end}
	for _, event := range events {
		accumulator.Add(event)
	}
	return accumulator.Result(end, reason, code, dropped)
}

func Plateau(results []Result) bool {
	if len(results) < 3 {
		return false
	}
	for i := len(results) - 2; i < len(results); i++ {
		previous, current := results[i-1], results[i]
		if current.Target <= previous.Target || previous.SuccessRPS <= 0 || (current.SuccessRPS-previous.SuccessRPS)/previous.SuccessRPS >= .05 {
			return false
		}
		worse := current.P95 > previous.P95 || current.P99 > previous.P99 || (current.FailureRate != nil && previous.FailureRate != nil && *current.FailureRate > *previous.FailureRate) || current.Backlog > previous.Backlog
		if !worse {
			return false
		}
	}
	return true
}

func (t Task) Validate() error {
	if t.SchemaVersion != TaskVersion || t.RunID == "" || t.GroupID == "" || t.Output == "" || t.Preparation == "" || t.SafetyFile == "" || t.VUs != 512 || t.Route < -1 || t.Route > 21 {
		return fmt.Errorf("负载任务无效")
	}
	if t.StartRPS < 1 || t.StartRPS > 6400 || t.MaxRPS != 6400 {
		return fmt.Errorf("起始 RPS 或扫描上限无效")
	}
	if t.Scan {
		return nil
	}
	if len(t.Stages) != 1 || t.Stages[0].Target < 1 || t.Stages[0].Target > 6400 || t.Stages[0].Warmup < 0 || t.Stages[0].Measurement < 1 {
		return fmt.Errorf("独立组负载无效")
	}
	return nil
}
