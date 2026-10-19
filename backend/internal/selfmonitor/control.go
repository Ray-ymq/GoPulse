// Package selfmonitor exposes a fixed systemd control boundary. It never takes
// commands, unit names, paths, or Docker options from HTTP clients.
package selfmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const Socket = "/run/gopulse-selfmonitor/control.sock"
const unit = "gopulse-selfmonitor.service"

// Status deliberately omits upstream errors and addresses, which can contain secrets.
type Status struct {
	State      string   `json:"state"`
	Reason     string   `json:"reason"`
	GrafanaURL string   `json:"grafana_url"`
	Targets    []Target `json:"targets"`
}
type Target struct {
	Job      string `json:"job"`
	Instance string `json:"instance"`
	Up       bool   `json:"up"`
}

func status(state, reason string) Status { return Status{state, reason, "/grafana/", []Target{}} }

type Runner func(context.Context, ...string) ([]byte, error)

// Controller is served only over a systemd-owned Unix socket. HTTP authorization
// belongs to the management API; filesystem permissions protect this boundary.
type Controller struct {
	run    Runner
	client *http.Client
	mu     sync.Mutex
}

func NewController() *Controller {
	return &Controller{run: func(ctx context.Context, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "/usr/bin/systemctl", args...).Output()
	}, client: &http.Client{Timeout: 250 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (s *Controller) unitState(ctx context.Context) (Status, error) {
	out, err := s.run(ctx, "show", unit, "--property=ActiveState,SubState,Result")
	if err != nil {
		return status("unavailable", "control_unavailable"), err
	}
	props := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			props[k] = v
		}
	}
	switch props["ActiveState"] {
	case "inactive":
		return status("stopped", "stopped"), nil
	case "failed":
		return status("failed", "unit_failed"), nil
	case "activating":
		return status("starting", "starting"), nil
	case "deactivating":
		return status("stopping", "stopping"), nil
	case "active":
		return status("running", "ok"), nil
	default:
		return status("unavailable", "control_unavailable"), errors.New("unknown unit state")
	}
}
func (s *Controller) snapshot(ctx context.Context) (Status, error) {
	state, err := s.unitState(ctx)
	if err != nil || state.State != "running" {
		return state, err
	}
	// A successful Compose process does not imply that its containers or targets
	// are healthy. Prometheus target health remains visible after backend restarts.
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Targets []struct {
				Labels struct {
					Job      string `json:"job"`
					Instance string `json:"instance"`
				} `json:"labels"`
				Health string `json:"health"`
			} `json:"activeTargets"`
		} `json:"data"`
	}
	if err = s.get(ctx, "http://127.0.0.1:29090/api/v1/targets?state=active", &payload); err != nil || payload.Status != "success" {
		return status("degraded", "prometheus_unavailable"), nil
	}
	expected := map[string]int{"backend": 3, "business-worker": 2, "search-indexer": 2, "monitor": 1, "marshaller": 2, "node": 1, "mysql": 1, "redis": 1, "rabbitmq": 1, "kafka": 1, "elasticsearch": 1, "observability-elasticsearch": 1, "victoriametrics": 1, "prometheus": 1}
	seen := map[string]int{}
	unique := map[string]bool{}
	for _, t := range payload.Data.Targets {
		if _, ok := expected[t.Labels.Job]; !ok {
			continue
		}
		key := t.Labels.Job + "/" + t.Labels.Instance
		if unique[key] {
			state.State = "degraded"
			continue
		}
		unique[key] = true
		seen[t.Labels.Job]++
		up := t.Health == "up"
		state.Targets = append(state.Targets, Target{t.Labels.Job, t.Labels.Instance, up})
		if !up {
			state.State = "degraded"
		}
	}
	for job, count := range expected {
		if seen[job] != count {
			state.State = "degraded"
		}
	}
	// up only reports a successful scrape. Exporters may return HTTP 200 while
	// their source is down; query source health separately as well.
	var sources struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Value []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	query := "http://127.0.0.1:29090/api/v1/query?query=" + url.QueryEscape(`count(mysql_up{job="mysql"} == 1) + count(redis_up{job="redis"} == 1) + count(elasticsearch_cluster_health_number_of_nodes{job=~"elasticsearch|observability-elasticsearch"} > 0)`)
	if s.get(ctx, query, &sources) != nil || sources.Status != "success" || len(sources.Data.Result) != 1 || len(sources.Data.Result[0].Value) != 2 || string(sources.Data.Result[0].Value[1]) != `"4"` {
		state.State = "degraded"
	}
	var grafana struct {
		Database string `json:"database"`
	}
	if s.get(ctx, "http://127.0.0.1:23000/grafana/api/health", &grafana) != nil || grafana.Database != "ok" {
		state.State = "degraded"
	}
	if state.State == "degraded" {
		state.Reason = "partial_unreachable"
	}
	return state, nil
}
func (s *Controller) get(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return errors.New("upstream unavailable")
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}
func (s *Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) > 0 {
		http.Error(w, "invalid request", 400)
		return
	}
	action := ""
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/status":
	case r.Method == http.MethodPost && r.URL.Path == "/start":
		action = "start"
	case r.Method == http.MethodPost && r.URL.Path == "/stop":
		action = "stop"
	default:
		http.Error(w, "not found", 404)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	var state Status
	var err error
	if action == "" {
		state, err = s.snapshot(ctx)
	} else {
		state, err = s.unitState(ctx)
		if err == nil {
			// Duplicate requests in the same direction are idempotent. An opposite
			// request during a transition reports the transition and is retried later.
			if (action == "start" && (state.State == "stopped" || state.State == "failed")) || (action == "stop" && (state.State == "running" || state.State == "failed")) {
				_, err = s.run(ctx, "--no-block", action, unit)
				if err == nil {
					if action == "start" {
						state = status("starting", "starting")
					} else {
						state = status("stopping", "stopping")
					}
				}
			}
		}
	}
	if err != nil {
		w.WriteHeader(503)
		state = status("unavailable", "control_unavailable")
	}
	_ = json.NewEncoder(w).Encode(state)
}

// Client speaks exclusively to the fixed Unix socket; it has no command runner.
type Client struct{ client *http.Client }

func NewClient() *Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", Socket)
	}}
	return &Client{&http.Client{Transport: transport, Timeout: 5 * time.Second}}
}
func (s *Client) Do(ctx context.Context, action string) (Status, error) {
	method := http.MethodPost
	switch action {
	case "status":
		method = http.MethodGet
	case "start", "stop":
	default:
		return status("unavailable", "control_unavailable"), errors.New("invalid action")
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://selfmonitor/"+action, nil)
	if err != nil {
		return Status{}, err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return status("unavailable", "control_unavailable"), err
	}
	defer func() { _ = res.Body.Close() }()
	var state Status
	if res.StatusCode != 200 {
		return status("unavailable", "control_unavailable"), fmt.Errorf("controller status %d", res.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&state)
	return state, err
}
