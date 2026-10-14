// Command business-fault-proxy is an acceptance-only, bounded proxy. It is
// intentionally not imported by product services and has no database client.
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
	"syscall"
	"time"
)

const (
	modeResponseAfterCommit = "response-after-commit"
	modeBrokerConfirm       = "broker-confirm"
	modeEffectBeforeDone    = "effect-before-completion"
	modeTelemetry           = "telemetry"
	modeDelay               = "delay"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:19124", "loopback listen address")
	upstream := flag.String("upstream", "", "upstream HTTP URL")
	mode := flag.String("mode", modeResponseAfterCommit, "fault mode")
	delay := flag.Duration("delay", 0, "bounded delay for delay mode")
	selfTest := flag.Bool("self-test", false, "validate the fixture contract without starting a server")
	flag.Parse()
	if *selfTest {
		if err := selfTestContract(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("[gopulse-fault-proxy] PASS: bounded acceptance fault contract")
		return
	}
	if err := validateConfig(*listen, *upstream, *mode, *delay); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	target, _ := url.Parse(*upstream)
	server := &http.Server{Addr: *listen, Handler: handler(target, *mode, *delay), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 20 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func validateConfig(listen, upstream, mode string, delay time.Duration) error {
	if host, _, err := net.SplitHostPort(listen); err != nil || (host != "127.0.0.1" && host != "localhost") {
		return fmt.Errorf("fault proxy must listen on a loopback address")
	}
	if _, ok := supportedModes[mode]; !ok {
		return fmt.Errorf("unsupported fault mode %q", mode)
	}
	if mode == modeDelay && (delay <= 0 || delay > 60*time.Second) {
		return fmt.Errorf("delay mode requires a duration from 1ns through 60s")
	}
	if upstream == "" {
		return fmt.Errorf("upstream is required")
	}
	target, err := url.Parse(upstream)
	if err != nil || target.Scheme != "http" || target.Host == "" {
		return fmt.Errorf("upstream must be an HTTP URL")
	}
	return nil
}

var supportedModes = map[string]bool{
	modeResponseAfterCommit: true,
	modeBrokerConfirm:       true,
	modeEffectBeforeDone:    true,
	modeTelemetry:           true,
	modeDelay:               true,
}

func handler(target *url.URL, mode string, delay time.Duration) http.Handler {
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body := io.LimitReader(request.Body, 1<<20)
		forward, err := http.NewRequestWithContext(request.Context(), request.Method, target.ResolveReference(&url.URL{Path: request.URL.Path, RawQuery: request.URL.RawQuery}).String(), body)
		if err != nil {
			http.Error(writer, "proxy request rejected", http.StatusBadGateway)
			return
		}
		forward.Header = request.Header.Clone()
		forward.Header.Set("X-GoPulse-Acceptance-Fault", mode)
		response, err := client.Do(forward)
		if err != nil {
			http.Error(writer, "proxy upstream unavailable", http.StatusBadGateway)
			return
		}
		defer func() { _ = response.Body.Close() }()
		if mode == modeDelay {
			timer := time.NewTimer(delay)
			select {
			case <-request.Context().Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
		if readErr != nil || len(payload) > 4<<20 {
			http.Error(writer, "proxy response rejected", http.StatusBadGateway)
			return
		}
		if shouldDrop(mode, response.Header) || shouldDropCommittedHTTP(mode, request, response.StatusCode, payload) {
			dropConnection(writer)
			return
		}
		for key, values := range response.Header {
			for _, value := range values {
				writer.Header().Add(key, value)
			}
		}
		if mode == modeEffectBeforeDone {
			writer.Header().Del("X-GoPulse-Completion")
			writer.Header().Set("X-GoPulse-Fault", "completion-missing-after-effect")
		}
		writer.WriteHeader(response.StatusCode)
		_, _ = writer.Write(payload)
	})
}

func shouldDropCommittedHTTP(mode string, request *http.Request, status int, payload []byte) bool {
	if mode != modeResponseAfterCommit || request.Method != http.MethodPost || request.URL.Path != "/api/v1/posts" || status != http.StatusCreated {
		return false
	}
	var envelope struct {
		Meta struct {
			CommandID string `json:"command_id"`
			EventID   string `json:"event_id"`
			PostID    uint64 `json:"post_id"`
		} `json:"meta"`
	}
	return json.Unmarshal(payload, &envelope) == nil && envelope.Meta.CommandID != "" && envelope.Meta.EventID != "" && envelope.Meta.PostID > 0
}

func shouldDrop(mode string, headers http.Header) bool {
	switch mode {
	case modeResponseAfterCommit:
		return strings.EqualFold(headerValue(headers, "X-GoPulse-Commit"), "durable")
	case modeBrokerConfirm:
		return strings.EqualFold(headerValue(headers, "X-GoPulse-Broker-Confirmation"), "durable")
	case modeTelemetry:
		return strings.EqualFold(headerValue(headers, "X-GoPulse-Telemetry"), "durable")
	default:
		return false
	}
}

func headerValue(headers http.Header, wanted string) string {
	for key, values := range headers {
		if strings.EqualFold(key, wanted) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
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

func selfTestContract() error {
	if err := validateConfig("127.0.0.1:19124", "http://127.0.0.1:8080", modeResponseAfterCommit, 0); err != nil {
		return err
	}
	if !shouldDrop(modeResponseAfterCommit, http.Header{"X-GoPulse-Commit": []string{"durable"}}) || shouldDrop(modeResponseAfterCommit, http.Header{"X-GoPulse-Commit": []string{"pending"}}) {
		return fmt.Errorf("commit response-drop rule is not deterministic")
	}
	if !shouldDrop(modeBrokerConfirm, http.Header{"X-GoPulse-Broker-Confirmation": []string{"durable"}}) {
		return fmt.Errorf("broker confirmation rule is not deterministic")
	}
	if !shouldDrop(modeTelemetry, http.Header{"X-GoPulse-Telemetry": []string{"durable"}}) {
		return fmt.Errorf("telemetry rule is not deterministic")
	}
	if err := validateConfig("0.0.0.0:19124", "http://127.0.0.1:8080", modeDelay, time.Second); err == nil {
		return fmt.Errorf("non-loopback listener was accepted")
	}
	return nil
}
