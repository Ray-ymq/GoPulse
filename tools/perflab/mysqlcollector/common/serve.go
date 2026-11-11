package common

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

func Serve(ctx context.Context, address string, scrape, shutdown time.Duration, handler http.Handler, logs *slog.Logger) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return errors.New("listen_failed")
	}
	defer func() { _ = listener.Close() }()
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/health", "/live", "/ready", "/startup":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			status, state := 200, "ok"
			if r.Method != "GET" {
				status, state = 405, "method_not_allowed"
				w.Header().Set("Allow", "GET")
			} else if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
				status, state = 400, "invalid_request"
			} else if r.URL.Path == "/ready" && ctx.Err() != nil {
				status, state = 503, "stopping"
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": state, "contract_version": "2"})
			return
		}
		handler.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: wrapped, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: scrape + time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	logs.Info("exporter started")
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("server_failed")
		}
		return nil
	case <-ctx.Done():
		drain, cancel := context.WithTimeout(context.Background(), shutdown)
		defer cancel()
		if server.Shutdown(drain) != nil {
			_ = server.Close()
			<-done
			return errors.New("shutdown_timeout")
		}
		<-done
		logs.Info("exporter stopped")
		return nil
	}
}
