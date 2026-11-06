package selfmonitor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestKubernetesControlNeverFallsBackToSystemd(t *testing.T) {
	c := NewController()
	c.cluster = &cluster{base: "https://cluster", namespace: "gopulse-all", token: "private-token", client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.Path, "/apis/apps/v1/namespaces/gopulse-all/") {
			t.Fatal(r.URL.Path)
		}
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"message":"private"}`))}, nil
	})}}
	c.run = func(context.Context, ...string) ([]byte, error) {
		t.Fatal("Kubernetes 失败时不得调用 systemd")
		return nil, nil
	}
	client := &Client{controller: c}
	for _, action := range []string{"status", "start", "stop"} {
		result, err := client.Do(context.Background(), action)
		if err == nil || result.State != "unavailable" || strings.Contains(err.Error(), "private") {
			t.Fatal(result, err)
		}
	}
}

func TestKubernetesStopOnlyPatchesFixedWorkloads(t *testing.T) {
	requests := 0
	k := &cluster{base: "https://cluster", namespace: "gopulse-business", token: "token"}
	k.client = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != "PATCH" || !strings.HasPrefix(r.URL.Path, "/apis/apps/v1/namespaces/gopulse-business/") || !strings.Contains(r.URL.Path, "selfmonitor-") {
			t.Fatal(r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(r.URL.Path, "daemonsets") {
			if !strings.Contains(string(raw), `"gopulse.io/selfmonitor-disabled":"true"`) {
				t.Fatal(string(raw))
			}
		} else if string(raw) != `{"spec":{"replicas":0}}` {
			t.Fatal(string(raw))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	if err := k.change(context.Background(), "stop"); err != nil {
		t.Fatal(err)
	}
	if requests != len(k.workloads()) {
		t.Fatal(requests)
	}
}

func TestKubernetesStoppingWaitsForDaemonSetPods(t *testing.T) {
	k := &cluster{base: "https://cluster", namespace: "gopulse-all"}
	k.client = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body := `{"spec":{"replicas":0},"status":{"replicas":0}}`
		if strings.Contains(r.URL.Path, "daemonsets") {
			body = `{"spec":{"template":{"spec":{"nodeSelector":{"gopulse.io/selfmonitor-disabled":"true"}}}},"status":{"numberReady":0,"currentNumberScheduled":1}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	result, err := k.state(context.Background())
	if err != nil || result.State != "stopping" {
		t.Fatal(result, err)
	}
}

func TestControlSelectionDoesNotInspectCredentialsForCompose(t *testing.T) {
	for _, kind := range []string{"", "compose"} {
		client, err := NewDeploymentClient(kind)
		if err != nil || client.controller != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewDeploymentClient("auto"); err == nil {
		t.Fatal("未知类型必须拒绝")
	}
}

func TestKubernetesHealthUsesOwnTargetsAndSources(t *testing.T) {
	c := NewController()
	k := &cluster{namespace: "gopulse-all"}
	c.cluster = k
	k.client = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"spec":{"replicas":1},"status":{"replicas":1,"readyReplicas":1,"numberReady":1}}`))}, nil
	})}
	for _, failure := range []string{"", "target", "source"} {
		c.client = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			body := `{"database":"ok"}`
			if strings.Contains(r.URL.Path, "targets") {
				targets := []map[string]any{}
				for job := range k.expectedTargets() {
					if job == "node" && failure == "target" {
						continue
					}
					targets = append(targets, map[string]any{"labels": map[string]string{"job": job, "instance": job + ":1"}, "health": "up"})
				}
				raw, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{"activeTargets": targets}})
				body = string(raw)
			} else if strings.Contains(r.URL.Path, "query") {
				body = `{"status":"success","data":{"result":[{"value":[1,"1"]}]}}`
				if failure == "source" {
					body = `{"status":"success","data":{"result":[]}}`
				}
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		state, err := c.snapshot(context.Background())
		expected := "running"
		if failure != "" {
			expected = "degraded"
		}
		if err != nil || state.State != expected {
			t.Fatal(failure, state, err)
		}
	}
}
