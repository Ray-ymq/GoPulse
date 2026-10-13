// Command http-effect-proxy is an acceptance-only HTTP proxy for a dependency
// whose successful effect must be observed before its response is lost.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type status struct {
	Mode             string `json:"mode"`
	RequestsSeen     uint64 `json:"requests_seen"`
	EffectObserved   uint64 `json:"effect_observed"`
	ResponseDropped  uint64 `json:"response_dropped"`
	ConnectionClosed uint64 `json:"connection_closed"`
}

type store struct {
	mu    sync.Mutex
	value status
}

func (state *store) update(update func(*status)) {
	state.mu.Lock()
	defer state.mu.Unlock()
	update(&state.value)
}

func (state *store) snapshot() status {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.value
}

func main() {
	listen := flag.String("listen", "0.0.0.0:9200", "HTTP proxy listener")
	control := flag.String("control", "127.0.0.1:19127", "status HTTP listener")
	upstream := flag.String("upstream", "", "upstream HTTP URL")
	mode := flag.String("mode", "effect-before-completion", "fault mode")
	flag.Parse()
	if *upstream == "" || *mode != "effect-before-completion" {
		fmt.Fprintln(os.Stderr, "upstream and effect-before-completion mode are required")
		os.Exit(2)
	}
	target, err := url.Parse(*upstream)
	if err != nil || target.Scheme != "http" || target.Host == "" {
		fmt.Fprintln(os.Stderr, "upstream must be an HTTP URL")
		os.Exit(2)
	}
	state := &store{value: status{Mode: *mode}}
	server := &http.Server{Addr: *listen, Handler: proxyHandler(target, state), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	controlServer := &http.Server{Addr: *control, Handler: controlHandler(state), ReadHeaderTimeout: 2 * time.Second}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer listener.Close()
	controlListener, err := net.Listen("tcp", *control)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer controlListener.Close()
	go func() { _ = controlServer.Serve(controlListener) }()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		_ = controlServer.Shutdown(shutdown)
	}()
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func controlHandler(state *store) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ready" {
			writer.WriteHeader(http.StatusOK)
			return
		}
		if request.URL.Path != "/status" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(state.snapshot())
	})
}

func proxyHandler(target *url.URL, state *store) http.Handler {
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(io.LimitReader(request.Body, 8<<20+1))
		if err != nil || len(body) > 8<<20 {
			http.Error(writer, "request body rejected", http.StatusBadRequest)
			return
		}
		upstreamRequest, err := http.NewRequestWithContext(request.Context(), request.Method, target.ResolveReference(&url.URL{Path: request.URL.Path, RawQuery: request.URL.RawQuery}).String(), strings.NewReader(string(body)))
		if err != nil {
			http.Error(writer, "proxy request rejected", http.StatusBadGateway)
			return
		}
		upstreamRequest.Header = request.Header.Clone()
		upstreamRequest.Header.Set("X-GoPulse-Acceptance-Fault", "effect-before-completion")
		response, err := client.Do(upstreamRequest)
		if err != nil {
			http.Error(writer, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
		if readErr != nil || len(payload) > 8<<20 {
			http.Error(writer, "response rejected", http.StatusBadGateway)
			return
		}
		state.update(func(value *status) { value.RequestsSeen++ })
		if isEffectRequest(target, request, response.StatusCode) {
			state.update(func(value *status) {
				value.EffectObserved++
				value.ResponseDropped++
				value.ConnectionClosed++
			})
			dropConnection(writer)
			return
		}
		for key, values := range response.Header {
			for _, value := range values {
				writer.Header().Add(key, value)
			}
		}
		writer.WriteHeader(response.StatusCode)
		_, _ = writer.Write(payload)
	})
}

func isEffectRequest(_ *url.URL, request *http.Request, status int) bool {
	return request != nil && request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/gopulse-post-search-v1/_doc/") && status >= 200 && status < 300
}

func dropConnection(writer http.ResponseWriter) {
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		return
	}
	connection, _, err := hijacker.Hijack()
	if err == nil {
		_ = connection.Close()
	}
}
