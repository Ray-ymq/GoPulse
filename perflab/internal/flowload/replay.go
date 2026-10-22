package flowload

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Replay 仅读取归档任务和事件，不启动 Docker 或负载。
func Replay(taskPath string) (Report, error) {
	var t Task
	data, err := os.ReadFile(taskPath)
	if err != nil {
		return Report{}, err
	}
	if err = json.Unmarshal(data, &t); err != nil {
		return Report{}, err
	}
	dir := filepath.Dir(taskPath)
	var archived Report
	data, err = os.ReadFile(filepath.Join(dir, "load-result.json"))
	if err != nil {
		return Report{}, err
	}
	if err = json.Unmarshal(data, &archived); err != nil {
		return Report{}, err
	}
	file, err := os.Open(filepath.Join(dir, "requests.jsonl"))
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = file.Close() }()
	accumulators := map[string]*Accumulator{}
	for index, old := range archived.Stages {
		warmup, measure := 30, 300
		if len(t.Stages) > 0 {
			warmup = t.Stages[0].Warmup
			measure = t.Stages[0].Measurement
		}
		if old.Name == "plateau-observation" {
			warmup = 0
		}
		accumulators[fmt.Sprintf("%s-%d", t.GroupID, index)] = &Accumulator{Stage: Stage{Name: old.Name, Target: old.Target, Warmup: warmup, Measurement: measure}, Scenario: old.Begin - float64(warmup)*1000, End: old.End}
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var e Event
		if err = json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return Report{}, err
		}
		key, _, ok := strings.Cut(e.RequestID, ":")
		if !ok {
			return Report{}, fmt.Errorf("请求标识无效")
		}
		if a := accumulators[key]; a != nil {
			a.Add(e)
		} else {
			return Report{}, fmt.Errorf("事件档位无效")
		}
	}
	if err = scanner.Err(); err != nil {
		return Report{}, err
	}
	result := Report{SchemaVersion: archived.SchemaVersion, StopReason: archived.StopReason, Offset: archived.Offset}
	for index, old := range archived.Stages {
		code := 0
		if !old.ExecutionCompleted && old.StopReason == "" {
			code = 1
		}
		r := accumulators[fmt.Sprintf("%s-%d", t.GroupID, index)].Result(old.ObservedEnd, old.StopReason, code, old.Dropped)
		r.Backlog = old.Backlog
		result.Stages = append(result.Stages, r)
	}
	return result, nil
}
