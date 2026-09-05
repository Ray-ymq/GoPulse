package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	mathrand "math/rand"
	"sync"
	"time"
)

const DefaultCapacity = 256

type Sender interface {
	PublishRaw(context.Context, string, any) error
}

type Config struct {
	Capacity int
	Timeout  time.Duration
	RetryMin time.Duration
	RetryMax time.Duration
	Now      func() time.Time
	Jitter   func(time.Duration) time.Duration
	Sender   Sender
	Logger   *slog.Logger
}

type queued struct {
	id   string
	body json.RawMessage
}

type Monitor struct {
	mu        sync.Mutex
	queue     []queued
	capacity  int
	closed    bool
	full      bool
	wake      chan struct{}
	done      chan struct{}
	stop      chan struct{}
	runCtx    context.Context
	cancel    context.CancelFunc
	timeout   time.Duration
	retryMin  time.Duration
	retryMax  time.Duration
	now       func() time.Time
	jitter    func(time.Duration) time.Duration
	sender    Sender
	logger    *slog.Logger
	transport bool
}

func NewMonitor(cfg Config) (*Monitor, error) {
	if cfg.Capacity == 0 {
		cfg.Capacity = DefaultCapacity
	}
	if cfg.Capacity < 1 || cfg.Capacity > 4096 {
		return nil, errors.New("event queue capacity must be between 1 and 4096")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("event publish timeout must be positive")
	}
	if cfg.RetryMin <= 0 {
		cfg.RetryMin = 100 * time.Millisecond
	}
	if cfg.RetryMax < cfg.RetryMin {
		cfg.RetryMax = 5 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Jitter == nil {
		rng := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
		var rngMu sync.Mutex
		cfg.Jitter = func(d time.Duration) time.Duration {
			rngMu.Lock()
			defer rngMu.Unlock()
			return time.Duration(rng.Int63n(int64(d/2) + 1))
		}
	}
	if cfg.Sender == nil {
		return nil, errors.New("event sender is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	runCtx, cancel := context.WithCancel(context.Background())
	m := &Monitor{capacity: cfg.Capacity, wake: make(chan struct{}, 1), done: make(chan struct{}), stop: make(chan struct{}), runCtx: runCtx, cancel: cancel, timeout: cfg.Timeout, retryMin: cfg.RetryMin, retryMax: cfg.RetryMax, now: cfg.Now, jitter: cfg.Jitter, sender: cfg.Sender, logger: cfg.Logger}
	go m.run()
	return m, nil
}

func (m *Monitor) Record(event Event) bool {
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return false
	}
	id := hex.EncodeToString(idBytes)
	body, err := CanonicalEnvelope(event, id, m.now())
	if err != nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	if len(m.queue) >= m.capacity {
		if !m.full {
			m.full = true
			m.logger.Warn("event queue unavailable", "module", "events", "event", "queue_full")
		}
		return false
	}
	m.queue = append(m.queue, queued{id: id, body: json.RawMessage(body)})
	m.signal()
	return true
}

func (m *Monitor) Close(ctx context.Context) error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.signal()
	}
	m.mu.Unlock()
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		m.cancel()
		closeOnce(m.stop)
		m.logger.Warn("event drain timed out", "module", "events", "event", "drain_timeout")
		return ctx.Err()
	}
}

func (m *Monitor) run() {
	defer close(m.done)
	backoff := m.retryMin
	for {
		m.mu.Lock()
		if len(m.queue) == 0 {
			if m.closed {
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()
			select {
			case <-m.wake:
			case <-m.stop:
				return
			}
			continue
		}
		item := m.queue[0]
		m.mu.Unlock()

		ctx, cancel := context.WithTimeout(m.runCtx, m.timeout)
		err := m.sender.PublishRaw(ctx, item.id, item.body)
		cancel()
		isPermanent := permanent(err)
		if err == nil || isPermanent {
			if isPermanent {
				m.logger.Warn("event record rejected", "module", "events", "event", "record_rejected")
			}
			m.mu.Lock()
			if len(m.queue) > 0 && m.queue[0].id == item.id {
				m.queue = m.queue[1:]
			}
			if m.full && len(m.queue) < m.capacity {
				m.full = false
				m.logger.Info("event queue available", "module", "events", "event", "queue_available")
			}
			if err == nil && m.transport {
				m.transport = false
				m.logger.Info("event transport recovered", "module", "events", "event", "transport_recovered")
			}
			m.mu.Unlock()
			backoff = m.retryMin
			continue
		}
		m.mu.Lock()
		if !m.transport {
			m.transport = true
			m.logger.Warn("event transport unavailable", "module", "events", "event", "transport_unavailable")
		}
		m.mu.Unlock()
		delay := backoff + m.jitter(backoff)
		if backoff < m.retryMax {
			backoff *= 2
			if backoff > m.retryMax {
				backoff = m.retryMax
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-m.stop:
			if !timer.Stop() {
				<-timer.C
			}
			return
		}
	}
}

func (m *Monitor) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

type permanentError interface{ Permanent() bool }

func permanent(err error) bool {
	var target permanentError
	return errors.As(err, &target) && target.Permanent()
}

func closeOnce(ch chan struct{}) {
	defer func() { _ = recover() }()
	close(ch)
}
