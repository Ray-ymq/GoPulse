package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	endpoint string
	token    string
	client   *http.Client
}

func NewHTTP(baseURL, token string, timeout time.Duration) (*HTTP, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("MONITOR_ROUTER_URL must be an HTTP base URL")
	}
	if len(token) < 32 || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("MONITOR_ROUTER_TOKEN must contain at least 32 bytes")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = timeout
	return &HTTP{
		endpoint: strings.TrimRight(baseURL, "/") + "/internal/v1/messages",
		token:    token,
		client:   &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (p *HTTP) Publish(ctx context.Context, message envelope.Envelope) error {
	return p.PublishRaw(ctx, message.MessageID, message)
}

func (p *HTTP) PublishRaw(ctx context.Context, messageID string, message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return errors.New("message serialization failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
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
