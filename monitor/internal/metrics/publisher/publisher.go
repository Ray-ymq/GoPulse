package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
)

type Publisher interface {
	Publish(context.Context, envelope.Envelope) error
}

type Transport interface {
	Publisher
	PublishRaw(context.Context, string, any) error
}

type RejectionError struct{ permanent bool }

func (e RejectionError) Error() string   { return "publisher rejected message" }
func (e RejectionError) Permanent() bool { return e.permanent }

type Discard struct{}

func (Discard) Publish(context.Context, envelope.Envelope) error { return nil }
func (Discard) PublishRaw(context.Context, string, any) error    { return nil }

type HTTP struct {
	endpoints []string
	token     string
	client    *http.Client
	next      atomic.Uint64
}

func NewHTTP(baseURL, token string, timeout time.Duration) (*HTTP, error) {
	return NewHTTPPool([]string{baseURL}, token, timeout)
}

func NewHTTPPool(baseURLs []string, token string, timeout time.Duration) (*HTTP, error) {
	if len(baseURLs) == 0 || len(baseURLs) > 8 {
		return nil, errors.New("MONITOR_ROUTER_URLS must contain 1 to 8 URLs")
	}
	if len(token) < 32 || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("MONITOR_ROUTER_TOKEN must contain at least 32 bytes")
	}
	if timeout <= 0 {
		return nil, errors.New("MONITOR_ROUTER_TIMEOUT must be positive")
	}
	endpoints := make([]string, 0, len(baseURLs))
	seen := make(map[string]struct{}, len(baseURLs))
	for _, baseURL := range baseURLs {
		normalized := strings.TrimSpace(baseURL)
		parsed, err := url.Parse(normalized)
		if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, errors.New("MONITOR_ROUTER_URLS must contain HTTP base URLs")
		}
		canonical := strings.TrimRight(normalized, "/")
		if _, ok := seen[canonical]; ok {
			return nil, errors.New("MONITOR_ROUTER_URLS must not contain duplicates")
		}
		seen[canonical] = struct{}{}
		endpoints = append(endpoints, canonical+"/internal/v1/messages")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = timeout
	return &HTTP{
		endpoints: endpoints,
		token:     token,
		client:    &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (p *HTTP) Publish(ctx context.Context, message envelope.Envelope) error {
	return p.PublishRaw(ctx, message.MessageID, message)
}

func (p *HTTP) PublishRaw(ctx context.Context, messageID string, message any) (result error) {
	body, err := json.Marshal(message)
	if err != nil {
		return errors.New("message serialization failed")
	}
	defer func() { componentmetrics.Dependency("router", result) }()
	if len(p.endpoints) == 0 {
		return errors.New("publisher has no Router endpoints")
	}
	start := int(p.next.Add(1)-1) % len(p.endpoints)
	for attempt := 0; attempt < len(p.endpoints); attempt++ {
		err := p.publishOnce(ctx, p.endpoints[(start+attempt)%len(p.endpoints)], messageID, body)
		if err == nil {
			return nil
		}
		var rejected RejectionError
		if errors.As(err, &rejected) && rejected.Permanent() {
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		result = err
	}
	return result
}

func (p *HTTP) publishOnce(ctx context.Context, endpoint, messageID string, body []byte) error {
	req, err := componentmetrics.NewRequest(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("publisher request failed")
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", messageID)
	response, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || isTimeout(err) {
			return errors.New("publisher request timed out")
		}
		return errors.New("publisher request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusAccepted {
		return RejectionError{permanent: response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests}
	}
	return nil
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
