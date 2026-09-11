// Package componentmetrics implements the private component metrics boundary.
// It deliberately does not register anything on the public application router.
package componentmetrics

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

const Path = "/internal/v1/metrics"

// Snapshot must return an immutable, already collected exposition. It must not
// perform dependency I/O. false means the component has no valid snapshot yet.
type Snapshot func() ([]byte, bool)

// NewHandler enforces authentication before revealing the routing contract.
// maxBytes is the component's frozen exposition budget, not a client setting.
func NewHandler(token string, maxBytes int, snapshot Snapshot) (http.Handler, error) {
	if len(token) < 32 || strings.ContainsAny(token, " \t\r\n") || maxBytes <= 0 || snapshot == nil {
		return nil, errors.New("invalid component metrics configuration")
	}
	expected := sha256.Sum256([]byte("Bearer " + token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		auth := r.Header.Values("Authorization")
		var supplied [32]byte
		if len(auth) == 1 {
			supplied = sha256.Sum256([]byte(auth[0]))
		}
		if len(auth) != 1 || subtle.ConstantTimeCompare(supplied[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.EscapedPath() != Path {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Reject unknown-length bodies too, without reading untrusted input or
		// waiting for a slow client. An empty chunked body is intentionally rejected.
		if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		body, ok := snapshot()
		if !ok || len(body) == 0 || len(body) > maxBytes {
			http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write(body)
	}), nil
}

// Listener owns only the private HTTP server. The caller owns the root context
// and supplies its shared shutdown deadline; no second signal handler is added.
type Listener struct {
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

// Address does not accept arbitrary interfaces, DNS names, or ports. Backend
// owns these three processes; the other modules own their respective listeners.
func Address(mode, component string) (string, error) {
	ports := map[string]string{"backend": "19101", "business-worker": "19102", "search-indexer": "19103"}
	port, ok := ports[component]
	if !ok {
		return "", errors.New("unknown component metrics component")
	}
	switch mode {
	case "host":
		return net.JoinHostPort("127.0.0.1", port), nil
	case "container":
		return net.JoinHostPort("0.0.0.0", port), nil
	default:
		return "", errors.New("invalid component metrics runtime mode")
	}
}

func start(ctx context.Context, address string, handler http.Handler) (*Listener, error) {
	if ctx == nil {
		return nil, errors.New("component metrics root context is required")
	}
	if handler == nil {
		return nil, errors.New("component metrics handler is required")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, errors.New("component metrics listener unavailable")
	}
	server := &http.Server{
		BaseContext: func(net.Listener) context.Context { return ctx },
		Handler:     handler, ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second,
		WriteTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096,
	}
	l := &Listener{server: server, done: make(chan struct{})}
	go func() { defer close(l.done); _ = server.Serve(listener) }()
	return l, nil
}

func (l *Listener) Shutdown(ctx context.Context) error {
	err := l.server.Shutdown(ctx)
	if err != nil {
		_ = l.server.Close()
	}
	<-l.done
	return err
}
