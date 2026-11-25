package platform

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Health 提供存活检查和有超时限制的依赖检查。
// check 必须响应上下文取消。
type Health struct {
	root    context.Context
	check   func(context.Context) error
	started atomic.Bool
}

func NewHealth(root context.Context, check func(context.Context) error) *Health {
	return &Health{root: root, check: check}
}

func (h *Health) Started() { h.started.Store(true) }

func (h *Health) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	status, state := http.StatusOK, "ok"
	switch r.URL.Path {
	case "/startup", "/live", "/health", "/ready":
		switch {
		case r.Method != http.MethodGet:
			status, state = http.StatusMethodNotAllowed, "method_not_allowed"
			w.Header().Set("Allow", "GET")
		case r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0:
			status, state = http.StatusBadRequest, "invalid_request"
		case r.URL.Path == "/startup" && !h.started.Load():
			status, state = http.StatusServiceUnavailable, "starting"
		case r.URL.Path == "/ready":
			if h.root.Err() != nil {
				status, state = http.StatusServiceUnavailable, "stopping"
			} else if !h.started.Load() {
				status, state = http.StatusServiceUnavailable, "not_ready"
			} else if h.check != nil {
				ctx, cancel := context.WithTimeout(r.Context(), time.Second)
				defer cancel()
				stop := context.AfterFunc(h.root, cancel)
				defer stop()
				err := h.check(ctx)
				if err != nil || ctx.Err() != nil || h.root.Err() != nil {
					status, state = http.StatusServiceUnavailable, "not_ready"
				}
			}
		}
	default:
		status, state = http.StatusNotFound, "not_found"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Status string `json:"status"`
	}{state})
}

// StartHealth 在指定地址启动健康检查；调用方负责关闭服务。
func StartHealth(address string, handler *Health) (*http.Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096}
	go func() { _ = server.Serve(listener) }()
	return server, nil
}
