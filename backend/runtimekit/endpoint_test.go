package runtimekit

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestFixedAddresses(t *testing.T) {
	for component, port := range map[string]string{"business-worker": "19102", "search-indexer": "19103"} {
		for mode, host := range map[string]string{"host": "127.0.0.1", "container": "0.0.0.0"} {
			got, err := Address(mode, component)
			if err != nil || got != net.JoinHostPort(host, port) {
				t.Fatalf("%s/%s: %q %v", mode, component, got, err)
			}
		}
	}
	if _, err := Address("host", "arbitrary"); err == nil {
		t.Fatal("arbitrary component accepted")
	}
	if _, err := Address("arbitrary", "business-worker"); err == nil {
		t.Fatal("arbitrary runtime mode accepted")
	}
}

func TestListenerStartupAndSharedShutdown(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = occupied.Close() }()
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") })
	if _, err := start(root, occupied.Addr().String(), handler); err == nil {
		t.Fatal("listener conflict not reported at startup")
	}
	listener, err := start(root, "127.0.0.1:0", handler)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := listener.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	select {
	case <-listener.done:
	default:
		t.Fatal("HTTP server still running")
	}
}
