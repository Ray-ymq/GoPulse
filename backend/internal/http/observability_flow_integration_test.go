//go:build integration && observability_integration

package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
)

func TestObservabilityFlowIntegration(t *testing.T) {
	cfg := integrationtest.ObservabilityEnvironment(t)
	adminUsername := mustObserveEnv(t, "OBSERVE_ADMIN_USERNAME")
	adminPassword := mustObserveEnv(t, "OBSERVE_ADMIN_PASSWORD")
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort)
	admin := newObserveClient(t)
	if status, body := observeJSON(t, admin, http.MethodPost, baseURL+"/api/v1/auth/login", `{"username":"`+adminUsername+`","password":"`+adminPassword+`"}`, ""); status != http.StatusOK {
		t.Fatalf("admin login status=%d body=%s", status, body)
	}

	user := newObserveClient(t)
	userUsername := strings.ReplaceAll(adminUsername, "observe_admin_", "observe_user_")
	userPassword := "observe-user-password-32-bytes-0123456789"
	status, body := observeJSON(t, user, http.MethodPost, baseURL+"/api/v1/auth/register", `{"username":"`+userUsername+`","password":"`+userPassword+`"}`, "")
	if status == http.StatusConflict {
		status, body = observeJSON(t, user, http.MethodPost, baseURL+"/api/v1/auth/login", `{"username":"`+userUsername+`","password":"`+userPassword+`"}`, "")
	}
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("ordinary-user setup status=%d body=%s", status, body)
	}

	unauthenticated := newObserveClient(t)
	for _, path := range []string{"/api/v1/observability/metrics?metric=gopulse_redis_up", "/api/v1/observability/logs", "/api/v1/observability/events"} {
		if status, body := observeJSON(t, unauthenticated, http.MethodGet, baseURL+path, "", ""); status != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s status=%d body=%s", path, status, body)
		}
		if status, body := observeJSON(t, user, http.MethodGet, baseURL+path, "", ""); status != http.StatusForbidden {
			t.Fatalf("ordinary user %s status=%d body=%s", path, status, body)
		}
	}

	commandStatus, commandBody := observeJSON(t, user, http.MethodGet, baseURL+"/api/v1/post-commands/context", "", "")
	if commandStatus != http.StatusOK {
		t.Fatalf("post command context status=%d body=%s", commandStatus, commandBody)
	}
	var commandContext struct {
		Data struct {
			DataEpoch string `json:"data_epoch"`
		} `json:"data"`
	}
	if err := json.Unmarshal(commandBody, &commandContext); err != nil || commandContext.Data.DataEpoch == "" {
		t.Fatalf("post command context body=%s err=%v", commandBody, err)
	}
	commandKey := fmt.Sprintf("123e4567-e89b-42d3-a456-%012x", time.Now().UnixNano()&0xffffffffffff)
	stageActionAt := time.Now().UTC()
	createStatus, createBody := observeJSONWithHeaders(t, user, http.MethodPost, baseURL+"/api/v1/posts", `{"title":"stage evidence post","content":"stage evidence content"}`, "", map[string]string{
		"Idempotency-Key":      commandKey,
		"X-GoPulse-Data-Epoch": commandContext.Data.DataEpoch,
	})
	if createStatus != http.StatusCreated {
		t.Fatalf("stage evidence post status=%d body=%s", createStatus, createBody)
	}
	var created struct {
		Meta struct {
			CommandID string `json:"command_id"`
			EventID   string `json:"event_id"`
			DataEpoch string `json:"data_epoch"`
			PostID    uint64 `json:"post_id"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(createBody, &created); err != nil || created.Meta.CommandID == "" || created.Meta.EventID == "" || created.Meta.DataEpoch != commandContext.Data.DataEpoch || created.Meta.PostID == 0 {
		t.Fatalf("stage evidence post response body=%s err=%v", createBody, err)
	}

	waitObserve(t, 90*time.Second, func() bool {
		from := stageActionAt.Add(-time.Second).Format(time.RFC3339Nano)
		to := time.Now().UTC().Format(time.RFC3339Nano)
		stageURL := baseURL + "/api/v1/observability/business-stages?epoch=" + url.QueryEscape(created.Meta.DataEpoch) + "&command_id=" + url.QueryEscape(created.Meta.CommandID) + "&check_visibility=true&include=logs,trace&from=" + url.QueryEscape(from) + "&to=" + url.QueryEscape(to)
		status, body := observeJSON(t, admin, http.MethodGet, stageURL, "", "")
		if status != http.StatusOK {
			return false
		}
		var stage struct {
			Data struct {
				Resolved struct {
					CommandID string `json:"command_id"`
					EventID   string `json:"event_id"`
					PostID    uint64 `json:"post_id"`
				} `json:"resolved"`
				Evidence struct {
					Command struct {
						Quality string `json:"quality"`
					} `json:"command"`
					Logs struct {
						Quality string `json:"quality"`
						Entries []struct {
							CommandID string `json:"command_id"`
							DataEpoch string `json:"data_epoch"`
						} `json:"entries"`
					} `json:"logs"`
					Visibility struct {
						Quality string `json:"quality"`
						Matched *bool  `json:"matched"`
					} `json:"visibility"`
				} `json:"evidence"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &stage) != nil {
			return false
		}
		if stage.Data.Resolved.CommandID != created.Meta.CommandID || stage.Data.Resolved.EventID != created.Meta.EventID || stage.Data.Resolved.PostID != created.Meta.PostID || stage.Data.Evidence.Command.Quality != "known" || stage.Data.Evidence.Visibility.Quality != "known" || stage.Data.Evidence.Visibility.Matched == nil || !*stage.Data.Evidence.Visibility.Matched || stage.Data.Evidence.Logs.Quality == "" {
			return false
		}
		for _, entry := range stage.Data.Evidence.Logs.Entries {
			if entry.CommandID == created.Meta.CommandID && entry.DataEpoch == created.Meta.DataEpoch {
				return true
			}
		}
		return false
	})

	requestID := fmt.Sprintf("%032x", time.Now().UnixNano())
	if status, body := observeJSON(t, user, http.MethodGet, baseURL+"/api/v1/posts", "", requestID); status != http.StatusOK {
		t.Fatalf("request that seeds log status=%d body=%s", status, body)
	}

	pluginPath := baseURL + "/api/v1/exporter-plugins/redis-exporter"
	if status, body := observeJSON(t, admin, http.MethodGet, pluginPath, "", ""); status != http.StatusOK {
		t.Fatalf("plugin status status=%d body=%s", status, body)
	}
	actionAt := time.Now().UTC()
	if status, body := observeJSON(t, admin, http.MethodPost, pluginPath+"/stop", "", ""); status != http.StatusOK {
		t.Fatalf("plugin stop status=%d body=%s", status, body)
	}
	if status, body := observeJSON(t, admin, http.MethodPost, pluginPath+"/start", "", ""); status != http.StatusOK {
		t.Fatalf("plugin start status=%d body=%s", status, body)
	}

	waitObserve(t, 90*time.Second, func() bool {
		status, body := observeJSON(t, admin, http.MethodGet, baseURL+"/api/v1/observability/metrics?metric=gopulse_redis_up", "", "")
		if status != http.StatusOK {
			return false
		}
		var result struct {
			Data struct {
				Series []struct {
					Labels struct {
						Source   string `json:"source"`
						TargetID string `json:"target_id"`
					} `json:"labels"`
					Points []struct {
						Timestamp string  `json:"timestamp"`
						Value     float64 `json:"value"`
					} `json:"points"`
				} `json:"series"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &result) != nil {
			return false
		}
		// The metric query service validates source/target_id in the VictoriaMetrics
		// adapter and intentionally omits those provenance labels from the public
		// Redis response. A post-action point therefore proves both the accepted
		// target identity and the current collection result at this API boundary.
		for _, series := range result.Data.Series {
			for _, point := range series.Points {
				at, err := time.Parse(time.RFC3339Nano, point.Timestamp)
				if err == nil && at.After(actionAt) && point.Value >= 0 {
					return true
				}
			}
		}
		return false
	})

	waitObserve(t, 90*time.Second, func() bool {
		status, body := observeJSON(t, admin, http.MethodGet, baseURL+"/api/v1/observability/logs?request_id="+url.QueryEscape(requestID)+"&limit=100", "", "")
		if status != http.StatusOK {
			return false
		}
		var page struct {
			Data []struct {
				RequestID string `json:"request_id"`
				Service   string `json:"service"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &page) != nil {
			return false
		}
		for _, entry := range page.Data {
			if entry.RequestID == requestID && entry.Service == "backend" {
				return true
			}
		}
		return false
	})

	waitObserve(t, 60*time.Second, func() bool {
		status, body := observeJSON(t, admin, http.MethodGet, baseURL+"/api/v1/observability/events?source=monitor&plugin_id=redis-exporter&limit=100", "", "")
		if status != http.StatusOK {
			return false
		}
		var page struct {
			Data []struct {
				EventName string `json:"event_name"`
				Timestamp string `json:"timestamp"`
				Source    string `json:"source"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &page) != nil {
			return false
		}
		stopped, started := false, false
		for _, entry := range page.Data {
			at, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
			if err != nil || entry.Source != "monitor" || !at.After(actionAt) {
				continue
			}
			stopped = stopped || entry.EventName == "exporter_plugin_stopped"
			started = started || entry.EventName == "exporter_plugin_started"
		}
		return stopped && started
	})
	t.Logf("observability flow passed request_id=%s action_at=%s metric=redis logs=backend events=plugin lifecycle", requestID, actionAt.Format(time.RFC3339Nano))
}

func newObserveClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

func observeJSON(t *testing.T, client *http.Client, method, endpoint, body, requestID string) (int, []byte) {
	return observeJSONWithHeaders(t, client, method, endpoint, body, requestID, nil)
}

func observeJSONWithHeaders(t *testing.T, client *http.Client, method, endpoint, body, requestID string, headers map[string]string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("HTTP %s %s: %v", method, endpoint, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, payload
}

func waitObserve(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("observability flow did not produce a current result within the bounded wait")
}

func mustObserveEnv(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required", key)
	}
	return value
}
