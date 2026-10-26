// Package containerstats 读取 cgroup v2 容器资源，供通用 exporter 和其他采集入口复用。
package containerstats

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const Schema = "gopulse.containerstats.v1"

type Target struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	PID  int    `json:"pid"`
}

type Sample struct {
	ID            string            `json:"id"`
	CPUMax        string            `json:"cpu_max"`
	CPU           map[string]uint64 `json:"cpu"`
	MemoryCurrent uint64            `json:"memory_current"`
	MemoryPeak    *uint64           `json:"memory_peak"`
	MemoryMax     *uint64           `json:"memory_max"`
	MemorySwapMax *uint64           `json:"memory_swap_max"`
	MemoryEvents  map[string]uint64 `json:"memory_events"`
	PIDsCurrent   uint64            `json:"pids_current"`
}

type Snapshot struct {
	SchemaVersion string            `json:"schema_version"`
	Epoch         float64           `json:"epoch"`
	Timestamp     string            `json:"timestamp"`
	Containers    map[string]Sample `json:"containers"`
}

type Collector struct {
	ProcFS      string
	CgroupFS    string
	TargetsFile string
}

func read(path string) (string, error) {
	b, err := os.ReadFile(path)
	return strings.TrimSpace(string(b)), err
}
func number(path string, unlimited bool) (*uint64, error) {
	raw, err := read(path)
	if err != nil {
		return nil, err
	}
	if unlimited && raw == "max" {
		return nil, nil
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	return &n, err
}
func counters(path string, required ...string) (map[string]uint64, error) {
	raw, err := read(path)
	if err != nil {
		return nil, err
	}
	out := map[string]uint64{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, errors.New("invalid cgroup counter")
		}
		if _, ok := out[fields[0]]; ok {
			return nil, errors.New("duplicate cgroup counter")
		}
		n, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return nil, err
		}
		out[fields[0]] = n
	}
	for _, key := range required {
		if _, ok := out[key]; !ok {
			return nil, fmt.Errorf("missing cgroup counter: %s", key)
		}
	}
	return out, nil
}
func (c Collector) path(pid int) (string, error) {
	raw, err := read(filepath.Join(c.ProcFS, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "0::") {
			relative := strings.TrimPrefix(line, "0::")
			if !strings.HasPrefix(relative, "/") {
				return "", errors.New("invalid cgroup path")
			}
			for _, part := range strings.Split(relative, "/") {
				if part == ".." {
					return "", errors.New("invalid cgroup path")
				}
			}
			return filepath.Join(c.CgroupFS, strings.TrimPrefix(relative, "/")), nil
		}
	}
	return "", errors.New("cgroup v2 is required")
}
func (c Collector) collect(target Target) (Sample, error) {
	var sample Sample
	path, err := c.path(target.PID)
	if err != nil {
		return sample, err
	}
	sample.ID = target.ID
	sample.CPUMax, err = read(filepath.Join(path, "cpu.max"))
	if err != nil {
		return sample, err
	}
	parts := strings.Fields(sample.CPUMax)
	if len(parts) != 2 {
		return sample, errors.New("invalid CPU quota")
	}
	period, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || period == 0 {
		return sample, errors.New("invalid CPU period")
	}
	if parts[0] != "max" {
		quota, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil || quota == 0 {
			return sample, errors.New("invalid CPU quota")
		}
	}
	sample.CPU, err = counters(filepath.Join(path, "cpu.stat"), "usage_usec", "nr_periods", "nr_throttled", "throttled_usec")
	if err != nil {
		return sample, err
	}
	current, err := number(filepath.Join(path, "memory.current"), false)
	if err != nil {
		return sample, err
	}
	sample.MemoryCurrent = *current
	sample.MemoryPeak, err = number(filepath.Join(path, "memory.peak"), false)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return sample, err
	}
	sample.MemoryMax, err = number(filepath.Join(path, "memory.max"), true)
	if err != nil {
		return sample, err
	}
	sample.MemorySwapMax, err = number(filepath.Join(path, "memory.swap.max"), true)
	if err != nil {
		return sample, err
	}
	sample.MemoryEvents, err = counters(filepath.Join(path, "memory.events"), "oom", "oom_kill")
	if err != nil {
		return sample, err
	}
	pids, err := number(filepath.Join(path, "pids.current"), false)
	if err != nil {
		return sample, err
	}
	sample.PIDsCurrent = *pids
	closing, err := c.path(target.PID)
	if err != nil || closing != path {
		return sample, errors.New("container cgroup changed")
	}
	return sample, nil
}

func label(value string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(value) + "\""
}

func validIdentity(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

func (c Collector) Collect() (Snapshot, error) {
	now := time.Now().UTC()
	snapshot := Snapshot{SchemaVersion: Schema, Epoch: float64(now.UnixNano()) / 1e9, Timestamp: now.Format(time.RFC3339Nano), Containers: map[string]Sample{}}
	f, err := os.Open(c.TargetsFile)
	if err != nil {
		return snapshot, err
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil || stat.Size() > 65536 {
		return snapshot, errors.New("invalid resource targets size")
	}
	var targets []Target
	decoder := json.NewDecoder(io.LimitReader(f, 65537))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&targets) != nil || len(targets) == 0 || len(targets) > 128 {
		return snapshot, errors.New("invalid resource targets")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return snapshot, errors.New("invalid resource targets")
	}
	for _, target := range targets {
		if !validIdentity(target.Name) || !validIdentity(target.ID) || target.PID <= 0 {
			return snapshot, errors.New("invalid resource target")
		}
		if _, ok := snapshot.Containers[target.Name]; ok {
			return snapshot, errors.New("duplicate resource target")
		}
		sample, err := c.collect(target)
		if err != nil {
			return snapshot, err
		}
		snapshot.Containers[target.Name] = sample
	}
	return snapshot, nil
}

func (c Collector) Handler() http.Handler {
	mux := http.NewServeMux()
	serve := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		snapshot, err := c.Collect()
		if err != nil {
			// 不向响应写入路径、目标文件内容或底层错误。
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "# TYPE gopulse_container_up gauge\ngopulse_container_up 0\n")
			return
		}
		if r.URL.Path == "/snapshot" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(snapshot)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = io.WriteString(w, "# TYPE gopulse_container_up gauge\ngopulse_container_up 1\n")
		// 每个 family 只输出一个 TYPE，按固定字段顺序输出所有实例。
		type field struct {
			name, kind string
			value      func(Sample) (float64, bool)
		}
		fields := []field{
			{"cpu_usage_seconds_total", "counter", func(s Sample) (float64, bool) { return float64(s.CPU["usage_usec"]) / 1e6, true }},
			{"cpu_periods_total", "counter", func(s Sample) (float64, bool) { return float64(s.CPU["nr_periods"]), true }},
			{"cpu_throttled_periods_total", "counter", func(s Sample) (float64, bool) { return float64(s.CPU["nr_throttled"]), true }},
			{"cpu_throttled_seconds_total", "counter", func(s Sample) (float64, bool) { return float64(s.CPU["throttled_usec"]) / 1e6, true }},
			{"memory_current_bytes", "gauge", func(s Sample) (float64, bool) { return float64(s.MemoryCurrent), true }},
			{"memory_peak_bytes", "gauge", func(s Sample) (float64, bool) {
				if s.MemoryPeak == nil {
					return 0, false
				}
				return float64(*s.MemoryPeak), true
			}},
			{"memory_limit_bytes", "gauge", func(s Sample) (float64, bool) {
				if s.MemoryMax == nil {
					return 0, false
				}
				return float64(*s.MemoryMax), true
			}},
			{"oom_events_total", "counter", func(s Sample) (float64, bool) { return float64(s.MemoryEvents["oom"]), true }},
			{"oom_kills_total", "counter", func(s Sample) (float64, bool) { return float64(s.MemoryEvents["oom_kill"]), true }},
			{"pids_current", "gauge", func(s Sample) (float64, bool) { return float64(s.PIDsCurrent), true }},
			{"cpu_quota_cores", "gauge", func(s Sample) (float64, bool) {
				p := strings.Fields(s.CPUMax)
				if p[0] == "max" {
					return 0, false
				}
				q, _ := strconv.ParseFloat(p[0], 64)
				period, _ := strconv.ParseFloat(p[1], 64)
				return q / period, true
			}},
		}
		for _, f := range fields {
			_, _ = fmt.Fprintf(w, "# TYPE gopulse_container_%s %s\n", f.name, f.kind)
			for name, sample := range snapshot.Containers {
				if value, ok := f.value(sample); ok {
					_, _ = fmt.Fprintf(w, "gopulse_container_%s{container=%s,container_id=%s} %g\n", f.name, label(name), label(sample.ID), value)
				}
			}
		}
	}
	mux.HandleFunc("/metrics", serve)
	mux.HandleFunc("/snapshot", serve)
	return mux
}
