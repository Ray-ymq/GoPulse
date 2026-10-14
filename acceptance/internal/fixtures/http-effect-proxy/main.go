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
	"strconv"
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
	TargetPostID     uint64 `json:"target_post_id"`
	TargetHit        bool   `json:"target_hit"`
}

type store struct {
	mu           sync.Mutex
	value        status
	targetPostID uint64
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

func (state *store) setPostTarget(postID uint64) error {
	if postID == 0 {
		return fmt.Errorf("target post ID must be positive")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.targetPostID != 0 && state.targetPostID != postID {
		return fmt.Errorf("target post ID is already set")
	}
	state.targetPostID = postID
	state.value.TargetPostID = postID
	return nil
}

func (state *store) shouldDrop(request *http.Request, responseStatus int) bool {
	if !isEffectRequest(nil, request, responseStatus) {
		return false
	}
	postID, ok := projectionPostID(request)
	if !ok {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.targetPostID == 0 || state.targetPostID != postID {
		return false
	}
	state.value.EffectObserved++
	state.value.ResponseDropped++
	state.value.ConnectionClosed++
	state.value.TargetHit = true
	return true
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
	defer func() { _ = listener.Close() }()
	controlListener, err := net.Listen("tcp", *control)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = controlListener.Close() }()
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
		if request.URL.Path == "/target" {
			if request.Method != http.MethodGet {
				writer.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			postID, err := strconv.ParseUint(request.URL.Query().Get("post_id"), 10, 64)
			if err != nil || postID == 0 {
				http.Error(writer, "post_id must be a positive integer", http.StatusBadRequest)
				return
			}
			if err := state.setPostTarget(postID); err != nil {
				http.Error(writer, err.Error(), http.StatusConflict)
				return
			}
			writer.WriteHeader(http.StatusNoContent)
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
		defer func() { _ = response.Body.Close() }()
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
		if readErr != nil || len(payload) > 8<<20 {
			http.Error(writer, "response rejected", http.StatusBadGateway)
			return
		}
		state.update(func(value *status) { value.RequestsSeen++ })
		if state.shouldDrop(request, response.StatusCode) {
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
	if request == nil || request.Method != http.MethodPut || status < 200 || status >= 300 {
		return false
	}
	_, ok := projectionPostID(request)
	return ok
}

func projectionPostID(request *http.Request) (uint64, bool) {
	if request == nil {
		return 0, false
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "gopulse-post-search-v1" || parts[1] != "_doc" {
		return 0, false
	}
	postID, err := strconv.ParseUint(parts[2], 10, 64)
	return postID, err == nil && postID > 0
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
