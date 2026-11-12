// Package runtimekit provides shared process health, lifecycle and HTTP support.
// It deliberately does not register anything on the public application router.
package runtimekit

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// Listener owns only the private HTTP server. The caller owns the root context
// and supplies its shared shutdown deadline; no second signal handler is added.
type Listener struct {
	probes *Probes
	server *http.Server
	done   chan struct{}
}

// Start binds synchronously so address conflicts are startup errors. A serving
// failure later does not cancel the component's business root context.
func Start(ctx context.Context, mode, component string, handler http.Handler) (*Listener, error) {
	address, err := Address(mode, component)
	if err != nil {
		return nil, err
	}
	return start(ctx, address, handler)
}

// Address does not accept arbitrary interfaces, DNS names, or ports. All
// processes share this fixed private-listener catalog.
func Address(mode, component string) (string, error) {
	ports := map[string]string{"business-worker": "19102", "search-indexer": "19103"}
	port, ok := ports[component]
	if !ok {
		return "", errors.New("unknown probe component")
	}
	switch mode {
	case "host":
		return net.JoinHostPort("127.0.0.1", port), nil
	case "container":
		return net.JoinHostPort("0.0.0.0", port), nil
	default:
		return "", errors.New("invalid probe runtime mode")
	}
}

func start(ctx context.Context, address string, handler http.Handler) (*Listener, error) {
	if ctx == nil {
		return nil, errors.New("probe root context is required")
	}
	if handler == nil {
		return nil, errors.New("probe handler is required")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, errors.New("probe listener unavailable")
	}
	server := &http.Server{
		BaseContext: func(net.Listener) context.Context { return ctx },
		Handler:     handler, ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096,
	}
	l := &Listener{server: server, done: make(chan struct{})}
	go func() { defer close(l.done); _ = server.Serve(listener) }()
	return l, nil
}

func (l *Listener) Shutdown(ctx context.Context) error {
	if l.probes != nil {
		l.probes.Stop()
	}
	err := l.server.Shutdown(ctx)
	if err != nil {
		_ = l.server.Close()
	}
	<-l.done
	return err
}
