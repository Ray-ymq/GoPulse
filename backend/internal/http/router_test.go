package http

import (
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeChecker struct {
	calls atomic.Int32
	check func(context.Context) error
}

func (checker *fakeChecker) Check(ctx context.Context) error {
	checker.calls.Add(1)
	if checker.check == nil {
		return nil
	}
	return checker.check(ctx)
}

func TestHealthReturnsExactContractWithoutCallingCheckers(t *testing.T) {
	mysql := &fakeChecker{}
	redis := &fakeChecker{}
	rabbitMQ := &fakeChecker{}
	router := NewRouter(Dependencies{MySQL: mysql, Redis: redis, RabbitMQ: rabbitMQ})

	response := performRequest(router, "/health")

	if response.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	assertJSONEqual(t, response.Body.String(), `{"status":"ok","service":"backend"}`)
	if mysql.calls.Load() != 0 || redis.calls.Load() != 0 || rabbitMQ.calls.Load() != 0 {
		t.Fatalf("/health invoked readiness checker: mysql=%d redis=%d rabbitmq=%d", mysql.calls.Load(), redis.calls.Load(), rabbitMQ.calls.Load())
	}
}

func TestReadyReturnsOKWhenAllDependenciesAreUp(t *testing.T) {
	router := NewRouter(Dependencies{
		MySQL:    &fakeChecker{},
		Redis:    &fakeChecker{},
		RabbitMQ: &fakeChecker{},
	})

	response := performRequest(router, "/ready")

	if response.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	assertReadyResponse(t, response.Body.String(), "ready", "up", "up", "up")
}

func TestReadyMarksSingleFailure(t *testing.T) {
	router := NewRouter(Dependencies{
		MySQL:    &fakeChecker{},
		Redis:    &fakeChecker{check: func(context.Context) error { return errors.New("redis unavailable") }},
		RabbitMQ: &fakeChecker{},
	})

	response := performRequest(router, "/ready")

	if response.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	assertReadyResponse(t, response.Body.String(), "not_ready", "up", "down", "up")
}

func TestReadyMarksMultipleFailures(t *testing.T) {
	router := NewRouter(Dependencies{
		MySQL:    &fakeChecker{check: func(context.Context) error { return errors.New("mysql unavailable") }},
		Redis:    &fakeChecker{},
		RabbitMQ: &fakeChecker{check: func(context.Context) error { return errors.New("rabbitmq unavailable") }},
	})

	response := performRequest(router, "/ready")

	if response.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	assertReadyResponse(t, response.Body.String(), "not_ready", "down", "up", "down")
}

func TestReadyTimesOutOneDependency(t *testing.T) {
	blocking := &fakeChecker{check: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	router := newRouter(Dependencies{
		MySQL:    &fakeChecker{},
		Redis:    blocking,
		RabbitMQ: &fakeChecker{},
	}, 30*time.Millisecond, 80*time.Millisecond)

	started := time.Now()
	response := performRequest(router, "/ready")
	elapsed := time.Since(started)

	if response.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("request took %s, want bounded timeout", elapsed)
	}
	assertReadyResponse(t, response.Body.String(), "not_ready", "up", "down", "up")
}

func TestReadyBoundsCheckerThatIgnoresContext(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	blocking := &fakeChecker{check: func(context.Context) error {
		<-release
		return nil
	}}
	router := newRouter(Dependencies{
		MySQL:    &fakeChecker{},
		Redis:    blocking,
		RabbitMQ: &fakeChecker{},
	}, 30*time.Millisecond, 80*time.Millisecond)

	started := time.Now()
	response := performRequest(router, "/ready")
	elapsed := time.Since(started)

	if response.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("request took %s, want bounded timeout", elapsed)
	}
	assertReadyResponse(t, response.Body.String(), "not_ready", "up", "down", "up")
}
func TestReadyRunsChecksConcurrently(t *testing.T) {
	delayed := func(context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	}
	router := newRouter(Dependencies{
		MySQL:    &fakeChecker{check: delayed},
		Redis:    &fakeChecker{check: delayed},
		RabbitMQ: &fakeChecker{check: delayed},
	}, 500*time.Millisecond, time.Second)

	started := time.Now()
	response := performRequest(router, "/ready")
	elapsed := time.Since(started)

	if response.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if elapsed >= 250*time.Millisecond {
		t.Fatalf("request took %s; checks appear sequential", elapsed)
	}
}

func TestReadyDoesNotExposeCheckerErrorsOrCredentials(t *testing.T) {
	secret := "super-secret-password"
	connectionURL := "amqp://user:" + secret + "@rabbitmq.internal:5672/"
	router := NewRouter(Dependencies{
		MySQL: &fakeChecker{},
		Redis: &fakeChecker{},
		RabbitMQ: &fakeChecker{check: func(context.Context) error {
			return errors.New("dial " + connectionURL + ": connection refused")
		}},
	})

	response := performRequest(router, "/ready")
	body := response.Body.String()

	if response.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	for _, forbidden := range []string{secret, connectionURL, "connection refused", "dial"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
	assertReadyResponse(t, body, "not_ready", "up", "up", "down")
}

func TestReadyTreatsMissingCheckerAsDown(t *testing.T) {
	router := NewRouter(Dependencies{MySQL: &fakeChecker{}, Redis: &fakeChecker{}})

	response := performRequest(router, "/ready")

	if response.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	assertReadyResponse(t, response.Body.String(), "not_ready", "up", "up", "down")
}

func performRequest(handler stdhttp.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(stdhttp.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertReadyResponse(t *testing.T, body, status, mysql, redis, rabbitMQ string) {
	t.Helper()
	assertJSONEqual(t, body, `{
		"status": "`+status+`",
		"service": "backend",
		"checks": {
			"mysql": "`+mysql+`",
			"redis": "`+redis+`",
			"rabbitmq": "`+rabbitMQ+`"
		}
	}`)
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
	actualJSON, _ := json.Marshal(actualValue)
	expectedJSON, _ := json.Marshal(expectedValue)
	if string(actualJSON) != string(expectedJSON) {
		t.Fatalf("JSON = %s, want %s", actualJSON, expectedJSON)
	}
}
