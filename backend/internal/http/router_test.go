package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/gin-gonic/gin"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type fakeChecker struct {
	calls atomic.Int32
	check func(context.Context) error
}

func (c *fakeChecker) Check(ctx context.Context) error {
	c.calls.Add(1)
	if c.check != nil {
		return c.check(ctx)
	}
	return nil
}
func TestRuntimeProbesHardAndSoftDependencies(t *testing.T) {
	down := &fakeChecker{check: func(context.Context) error { return errors.New("unavailable") }}
	mysql := &fakeChecker{}
	router := NewRouter(Dependencies{MySQL: mysql, Redis: down, RabbitMQ: down, Elasticsearch: down})
	live := performRequest(router, "/live")
	health := performRequest(router, "/health")
	if live.Code != 200 || live.Body.String() != health.Body.String() || mysql.calls.Load() != 0 {
		t.Fatal("liveness contract")
	}
	ready := performRequest(router, "/ready")
	if ready.Code != 200 || down.calls.Load() != 0 {
		t.Fatal("soft dependencies blocked readiness")
	}
	hard := NewRouter(Dependencies{MySQL: down})
	if performRequest(hard, "/ready").Code != 503 {
		t.Fatal("hard failure ready")
	}
	missing := NewRouter(Dependencies{})
	if performRequest(missing, "/ready").Code != 503 {
		t.Fatal("missing dependency ready")
	}
}
func performRequest(handler stdhttp.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(stdhttp.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertJSONEqual(t *testing.T, actual, expected string) {
	t.Helper()
	var actualValue any
	if err := json.Unmarshal([]byte(actual), &actualValue); err != nil {
		t.Fatalf("actual response is invalid JSON: %v", err)
	}
	var expectedValue any
	if err := json.Unmarshal([]byte(expected), &expectedValue); err != nil {
		t.Fatalf("test expectation is invalid JSON: %v", err)
	}
	if obj, ok := actualValue.(map[string]any); ok {
		if body, ok := obj["error"].(map[string]any); ok {
			if id, present := body["request_id"]; present {
				if !componentmetrics.ValidRequestID(id.(string)) {
					t.Fatal("invalid error request ID")
				}
				if expectedObj, ok := expectedValue.(map[string]any); ok {
					if expectedErr, ok := expectedObj["error"].(map[string]any); ok {
						if _, explicit := expectedErr["request_id"]; !explicit {
							delete(body, "request_id")
						}
					}
				}
			}
		}
	}
	actualJSON, _ := json.Marshal(actualValue)
	expectedJSON, _ := json.Marshal(expectedValue)
	if string(actualJSON) != string(expectedJSON) {
		t.Fatalf("JSON = %s, want %s", actualJSON, expectedJSON)
	}
}

func TestDevelopmentRouterSuppressesGinFrameworkOutput(t *testing.T) {
	originalMode := gin.Mode()
	originalWriter := gin.DefaultWriter
	originalErrorWriter := gin.DefaultErrorWriter
	t.Cleanup(func() {
		gin.DefaultWriter = originalWriter
		gin.DefaultErrorWriter = originalErrorWriter
		gin.SetMode(originalMode)
	})

	var frameworkOutput bytes.Buffer
	gin.DefaultWriter = &frameworkOutput
	gin.DefaultErrorWriter = &frameworkOutput
	if err := ConfigureGinMode("development"); err != nil {
		t.Fatalf("ConfigureGinMode(development) error = %v", err)
	}
	NewRouter(Dependencies{})

	if frameworkOutput.Len() != 0 {
		t.Fatalf("Gin framework output = %q", frameworkOutput.String())
	}
}

func TestConfigureGinMode(t *testing.T) {
	original := gin.Mode()
	t.Cleanup(func() { gin.SetMode(original) })

	for _, test := range []struct {
		appEnv string
		want   string
	}{
		{appEnv: "development", want: gin.DebugMode},
		{appEnv: "test", want: gin.TestMode},
		{appEnv: "production", want: gin.ReleaseMode},
	} {
		if err := ConfigureGinMode(test.appEnv); err != nil {
			t.Fatalf("ConfigureGinMode(%q) error = %v", test.appEnv, err)
		}
		if gin.Mode() != test.want {
			t.Fatalf("Gin mode = %q, want %q", gin.Mode(), test.want)
		}
	}

	if err := ConfigureGinMode("staging"); err == nil {
		t.Fatal("ConfigureGinMode(staging) error = nil")
	}
}
