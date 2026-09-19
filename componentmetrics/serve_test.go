package componentmetrics

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestRuntimeDrainsInFlightAndReportsTimeout(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "timeout"}[timeout], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(204) })}
			budget := time.Second
			if timeout {
				budget = 20 * time.Millisecond
			}
			done := make(chan error, 1)
			go func() { done <- ServeRuntime(ctx, server, listener, budget) }()
			clientDone := make(chan struct{})
			go func() {
				defer close(clientDone)
				res, err := http.Get("http://" + listener.Addr().String() + "/work")
				if err == nil {
					res.Body.Close()
				}
			}()
			<-entered
			cancel()
			if !timeout {
				close(release)
			}
			select {
			case err := <-done:
				if (err != nil) != timeout {
					t.Fatalf("shutdown=%v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("unbounded drain")
			}
			if timeout {
				close(release)
			}
			<-clientDone
		})
	}
}
