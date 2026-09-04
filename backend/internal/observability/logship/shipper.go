package logship

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const responseLimit = 4096

type permanentDeliveryError struct{}

func (permanentDeliveryError) Error() string { return "delivery permanently rejected" }

type Config struct {
	Endpoint        string
	Token           string
	RequestTimeout  time.Duration
	QueueCapacity   int
	RetryMin        time.Duration
	RetryMax        time.Duration
	ShutdownTimeout time.Duration
}

type item struct {
	id   string
	body []byte
}

type Shipper struct {
	endpoint string
	token    string
	client   *http.Client
	queue    chan item
	retryMin time.Duration
	retryMax time.Duration
	logger   *slog.Logger
	closing  chan struct{}
	done     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	closed   atomic.Bool
	close    sync.Once
}

func New(cfg Config, logger *slog.Logger) (*Shipper, error) {
	if cfg.Endpoint == "" || cfg.Token == "" || cfg.RequestTimeout <= 0 || cfg.QueueCapacity <= 0 || cfg.RetryMin <= 0 || cfg.RetryMax < cfg.RetryMin || cfg.ShutdownTimeout <= 0 {
		return nil, errors.New("invalid log shipper configuration")
	}
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = cfg.RequestTimeout
	workerContext, workerCancel := context.WithCancel(context.Background())
	s := &Shipper{
		endpoint: cfg.Endpoint, token: cfg.Token,
		client: &http.Client{Timeout: cfg.RequestTimeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		queue:  make(chan item, cfg.QueueCapacity), retryMin: cfg.RetryMin, retryMax: cfg.RetryMax,
		logger: logger, closing: make(chan struct{}), done: make(chan struct{}), ctx: workerContext, cancel: workerCancel,
	}
	go s.run()
	return s, nil
}

func (s *Shipper) Enqueue(body []byte) bool {
	if s == nil || s.closed.Load() || len(body) == 0 {
		return false
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		s.logger.Warn("log remote copy dropped", "reason", "message_id_unavailable")
		return false
	}
	entry := item{id: hex.EncodeToString(idBytes), body: append([]byte(nil), body...)}
	select {
	case s.queue <- entry:
		return true
	default:
		s.logger.Warn("log remote copy dropped", "reason", "queue_full")
		return false
	}
}

func (s *Shipper) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.close.Do(func() {
		s.closed.Store(true)
		close(s.closing)
	})
	select {
	case <-s.done:
		s.cancel()
		return nil
	case <-ctx.Done():
		s.cancel()
		s.client.CloseIdleConnections()
		<-s.done
		return errors.New("log shipper shutdown timed out")
	}
}

func (s *Shipper) run() {
	defer close(s.done)
	for {
		select {
		case entry := <-s.queue:
			if !s.deliver(entry) {
				return
			}
		case <-s.closing:
			for {
				select {
				case entry := <-s.queue:
					if !s.deliver(entry) {
						return
					}
				default:
					return
				}
			}
		}
	}
}

func (s *Shipper) deliver(entry item) bool {
	delay := s.retryMin
	unavailableReported := false
	for {
		ctx, cancel := context.WithTimeout(s.ctx, s.client.Timeout)
		err := s.send(ctx, entry)
		cancel()
		if err == nil {
			if unavailableReported {
				s.logger.Info("log remote delivery restored", "reason", "recovered")
			}
			return true
		}
		var permanent permanentDeliveryError
		if errors.As(err, &permanent) {
			s.logger.Warn("log remote copy dropped", "reason", "permanent_rejection")
			return true
		}
		if !unavailableReported {
			s.logger.Warn("log remote delivery will retry", "reason", "transport_unavailable")
			unavailableReported = true
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-s.ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false
		}
		if delay < s.retryMax {
			delay *= 2
			if delay > s.retryMax {
				delay = s.retryMax
			}
		}
	}
}

func (s *Shipper) send(ctx context.Context, entry item) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(entry.body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", entry.id)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil || len(body) > responseLimit {
		return errors.New("delivery response invalid")
	}
	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return permanentDeliveryError{}
	default:
		return errors.New("delivery rejected")
	}
}

func IsTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
