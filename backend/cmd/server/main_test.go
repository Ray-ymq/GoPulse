package main

import (
	"context"
	"errors"
	"net"
	stdhttp "net/http"
	"testing"
	"time"
)

func TestServeGracefullyShutsDownAndReleasesPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	address := listener.Addr().String()

	server := &stdhttp.Server{
		Addr: address,
		Handler: stdhttp.HandlerFunc(func(response stdhttp.ResponseWriter, _ *stdhttp.Request) {
			response.WriteHeader(stdhttp.StatusNoContent)
		}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, server, func() error { return server.Serve(listener) })
	}()

	client := stdhttp.Client{Timeout: time.Second}
	deadline := time.Now().Add(2 * time.Second)
	for {
		response, requestErr := client.Get("http://" + address)
		if requestErr == nil {
			_ = response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become reachable: %v", requestErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve() did not finish after cancellation")
	}

	rebound, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("port was not released after shutdown: %v", err)
	}
	_ = rebound.Close()
}

func TestServeReturnsStartupFailure(t *testing.T) {
	expected := errors.New("bind failed")
	server := &stdhttp.Server{Addr: "127.0.0.1:0"}

	err := serve(context.Background(), server, func() error { return expected })
	if !errors.Is(err, expected) {
		t.Fatalf("serve() error = %v, want startup failure", err)
	}
}
