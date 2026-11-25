package platform

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthLifecycleAndDependencies(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	var failure error
	calls := 0
	health := NewHealth(root, func(context.Context) error { calls++; return failure })
	check := func(path string, want int, body string) {
		t.Helper()
		response := httptest.NewRecorder()
		health.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want || response.Body.String() != body+"\n" {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
	}
	check("/startup", 503, `{"status":"starting"}`)
	check("/ready", 503, `{"status":"not_ready"}`)
	health.Started()
	check("/startup", 200, `{"status":"ok"}`)
	check("/ready", 200, `{"status":"ok"}`)
	failure = errors.New("数据库不可用")
	check("/ready", 503, `{"status":"not_ready"}`)
	check("/live", 200, `{"status":"ok"}`)
	check("/health", 200, `{"status":"ok"}`)
	if calls != 2 {
		t.Fatalf("检查次数 = %d", calls)
	}
	cancel()
	check("/ready", 503, `{"status":"stopping"}`)
}

func TestHealthCheckHonorsRequestCancellation(t *testing.T) {
	health := NewHealth(context.Background(), func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	health.Started()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	response := httptest.NewRecorder()
	health.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ready", nil).WithContext(ctx))
	if response.Code != 503 {
		t.Fatalf("状态码 = %d", response.Code)
	}
}

func TestHealthListenerReportsBindConflictAndCloses(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	if _, err := StartHealth(listener.Addr().String(), NewHealth(context.Background(), nil)); err == nil {
		t.Fatal("监听冲突未返回错误")
	}
	server, err := StartHealth("127.0.0.1:0", NewHealth(context.Background(), nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
