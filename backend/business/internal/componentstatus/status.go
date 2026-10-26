// Package componentstatus probes component liveness independently from
// telemetry collection. It deliberately has no database, metrics, or Monitor
// dependency: a missing data stream cannot turn a live process red.
package componentstatus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/runtimekit"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	DefaultInterval     = 5 * time.Second
	DefaultTimeout      = 500 * time.Millisecond
	DefaultTTL          = 15 * time.Second
	DefaultConcurrency  = 8
	maxProbeResponseLen = 4096
)

type Status struct {
	Component  string     `json:"component"`
	Instance   string     `json:"instance"`
	Status     string     `json:"status"`
	ObservedAt *time.Time `json:"observed_at"`
	ReasonCode string     `json:"reason_code"`
}

type Target struct {
	Component string
	Instance  string
	Probe     func(context.Context) error
}

type Config struct {
	Targets     []Target
	Interval    time.Duration
	Timeout     time.Duration
	TTL         time.Duration
	Concurrency int
	Now         func() time.Time
}

type record struct {
	status     string
	observedAt time.Time
	reason     string
}

type Service struct {
	targets     []Target
	interval    time.Duration
	timeout     time.Duration
	ttl         time.Duration
	concurrency int
	now         func() time.Time
	client      *http.Client

	mu      sync.RWMutex
	results map[string]record
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	closed  bool
}

func New(cfg Config) (*Service, error) {
	if len(cfg.Targets) == 0 {
		return nil, errors.New("component status targets are required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	seen := make(map[string]struct{}, len(cfg.Targets))
	targets := make([]Target, 0, len(cfg.Targets))
	for _, target := range cfg.Targets {
		if target.Component == "" || target.Instance == "" || target.Probe == nil {
			return nil, errors.New("component status target is incomplete")
		}
		key := target.Component + "\x00" + target.Instance
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate component status target %s/%s", target.Component, target.Instance)
		}
		seen[key] = struct{}{}
		targets = append(targets, target)
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Component != targets[j].Component {
			return targets[i].Component < targets[j].Component
		}
		return targets[i].Instance < targets[j].Instance
	})
	return &Service{targets: targets, interval: cfg.Interval, timeout: cfg.Timeout, ttl: cfg.TTL, concurrency: cfg.Concurrency, now: cfg.Now, client: &http.Client{Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, results: make(map[string]record)}, nil
}

func (s *Service) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("component status context is required")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("component status service is closed")
	}
	if s.cancel != nil {
		s.mu.Unlock()
		return nil
	}
	root, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		_ = s.Refresh(root)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-root.Done():
				return
			case <-ticker.C:
				_ = s.Refresh(root)
			}
		}
	}()
	return nil
}

func (s *Service) Refresh(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sem := make(chan struct{}, s.concurrency)
	var wg sync.WaitGroup
	for _, target := range s.targets {
		target := target
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			probeCtx, cancel := context.WithTimeout(ctx, s.timeout)
			err := safeProbe(probeCtx, target.Probe)
			cancel()
			status, reason := "healthy", "ok"
			if err != nil {
				status, reason = "unavailable", reasonCode(probeCtx, err)
			}
			now := s.now().UTC()
			key := target.Component + "\x00" + target.Instance
			s.mu.Lock()
			s.results[key] = record{status: status, observedAt: now, reason: reason}
			s.mu.Unlock()
		}()
	}
	wg.Wait()
	return ctx.Err()
}

func (s *Service) Snapshot() []Status {
	now := s.now().UTC()
	result := make([]Status, 0, len(s.targets))
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, target := range s.targets {
		key := target.Component + "\x00" + target.Instance
		item := Status{Component: target.Component, Instance: target.Instance, Status: "unknown", ReasonCode: "not_probed"}
		if observed, ok := s.results[key]; ok {
			item.Status, item.ReasonCode = observed.status, observed.reason
			at := observed.observedAt
			item.ObservedAt = &at
			if now.Sub(observed.observedAt) > s.ttl {
				item.Status, item.ReasonCode = "unknown", "stale"
			}
		}
		result = append(result, item)
	}
	return result
}

func (s *Service) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		if s.cancel != nil {
			s.cancel()
		}
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) Targets() []Target { return append([]Target(nil), s.targets...) }

func safeProbe(ctx context.Context, probe func(context.Context) error) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("probe_panicked")
		}
	}()
	return probe(ctx)
}

func reasonCode(ctx context.Context, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "probe_canceled"
	}
	return "connection_failed"
}

func HTTPProbe(client *http.Client, rawURL string) func(context.Context) error {
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	return func(ctx context.Context) error {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("invalid_probe_url")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(rawURL, "/")+"/live", nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = response.Body.Close() }()
		_, _ = io.CopyN(io.Discard, response.Body, maxProbeResponseLen)
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("probe returned HTTP %d", response.StatusCode)
		}
		return nil
	}
}

func KafkaProbe(brokers []string) func(context.Context) error {
	return func(ctx context.Context) error {
		if len(brokers) == 0 {
			return errors.New("kafka brokers are not configured")
		}
		client, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ClientID("gopulse-component-status"))
		if err != nil {
			return err
		}
		defer client.Close()
		if err := client.Ping(ctx); err != nil {
			return err
		}
		request := kmsg.NewPtrMetadataRequest()
		request.Topics = nil
		_, err = client.Request(ctx, request)
		return err
	}
}

func TargetsFromEnvironment() ([]Target, error) {
	mode := runtimekit.Mode()
	ports := map[string]string{"backend": "19101", "business-worker": "19102", "search-indexer": "19103", "monitor": "19104", "marshaller": "19106"}
	targets := make([]Target, 0, 16)
	for _, component := range runtimekit.Components {
		endpoints, err := runtimekit.ReplicaEndpoints(component)
		if err != nil {
			return nil, err
		}
		for _, endpoint := range endpoints {
			host := endpoint
			if mode == "host" && (endpoint == component || endpoint == component+"-local") {
				host = "127.0.0.1"
			}
			targets = append(targets, Target{Component: component, Instance: endpoint, Probe: HTTPProbe(nil, "http://"+host+":"+ports[component])})
		}
	}
	brokers, err := kafkaBrokersFromEnvironment(mode)
	if err != nil {
		return nil, err
	}
	targets = append(targets, Target{Component: "kafka", Instance: "kafka", Probe: KafkaProbe(brokers)})
	return targets, nil
}

func kafkaBrokersFromEnvironment(mode string) ([]string, error) {
	raw := strings.TrimSpace(os.Getenv("MARSHALLER_KAFKA_BROKERS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("MONITOR_KAFKA_BROKERS"))
	}
	if raw == "" {
		if mode == "container" {
			raw = "kafka:19092"
		} else {
			raw = "127.0.0.1:9092"
		}
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New("kafka broker list contains an empty entry")
		}
		result = append(result, part)
	}
	return result, nil
}
