package componentmetrics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
)

// RuntimeContractVersion identifies the wire semantics of the runtime probes.
const RuntimeContractVersion = "1"

// Probes owns startup/readiness state and a single dependency-check slot.
// A checker must honor its context. Even a broken checker that does not return
// occupies only that slot: requests time out without spawning further checkers.
type Probes struct {
	root                     context.Context
	timeout, ttl             time.Duration
	check                    func(context.Context) error
	mu                       sync.Mutex
	started, stopping, ready bool
	expires                  time.Time
	flight                   chan struct{}
}

func NewProbes(root context.Context, timeout, ttl time.Duration, check func(context.Context) error) (*Probes, error) {
	if root == nil || timeout <= 0 || ttl <= 0 {
		return nil, errors.New("invalid probe configuration")
	}
	return &Probes{root: root, timeout: timeout, ttl: ttl, check: check}, nil
}

// Started is called only after local initialization has completed.
func (p *Probes) Started() { p.mu.Lock(); p.started = true; p.mu.Unlock() }

// Stop withdraws readiness before the caller starts draining resources.
func (p *Probes) Stop() { p.mu.Lock(); p.stopping = true; p.mu.Unlock() }

func (p *Probes) readiness(ctx context.Context) bool {
	p.mu.Lock()
	if !p.started || p.stopping || p.root.Err() != nil {
		p.mu.Unlock()
		return false
	}
	if time.Now().Before(p.expires) {
		ready := p.ready
		p.mu.Unlock()
		return ready
	}
	if p.flight == nil {
		p.flight = make(chan struct{})
		done := p.flight
		checkCtx, cancel := context.WithTimeout(p.root, p.timeout)
		go func() {
			defer cancel()
			var err error
			if p.check != nil {
				err = runProbeCheck(checkCtx, p.check)
			}
			p.mu.Lock()
			p.ready = err == nil && checkCtx.Err() == nil
			p.expires = time.Now().Add(p.ttl)
			p.flight = nil
			close(done)
			p.mu.Unlock()
		}()
	}
	done := p.flight
	p.mu.Unlock()
	timer := time.NewTimer(p.timeout)
	defer timer.Stop()
	select {
	case <-done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.ready && !p.stopping && p.root.Err() == nil
	case <-ctx.Done():
		return false
	case <-p.root.Done():
		return false
	case <-timer.C:
		return false
	}
}

func (p *Probes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	status, state := http.StatusOK, "ok"
	switch r.URL.EscapedPath() {
	case "/startup", "/live", "/ready", "/health":
		if r.Method != http.MethodGet {
			status, state = http.StatusMethodNotAllowed, "method_not_allowed"
			w.Header().Set("Allow", "GET")
		} else if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			status, state = http.StatusBadRequest, "invalid_request"
		} else {
			p.mu.Lock()
			started, stopping := p.started, p.stopping || p.root.Err() != nil
			p.mu.Unlock()
			switch {
			case r.URL.Path == "/ready" && stopping:
				status, state = http.StatusServiceUnavailable, "stopping"
			case r.URL.Path == "/startup" && !started:
				status, state = http.StatusServiceUnavailable, "starting"
			case r.URL.Path == "/ready" && !p.readiness(r.Context()):
				status, state = http.StatusServiceUnavailable, "not_ready"
			}
		}
	default:
		status, state = http.StatusNotFound, "not_found"
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Status          string `json:"status"`
		ContractVersion string `json:"contract_version"`
	}{state, RuntimeContractVersion})
}

// Wrap keeps probes on the caller's existing listener, without changing other
// routes or their authentication boundary.
func (p *Probes) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/startup", "/live", "/ready", "/health":
			p.ServeHTTP(w, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func runProbeCheck(ctx context.Context, check func(context.Context) error) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("dependency_unavailable")
		}
	}()
	return check(ctx)
}
