package platform

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/elastic/go-elasticsearch/v9"
)

// Elasticsearch owns the bounded transport shared by readiness, search, and
// reindex callers. API-specific request and response validation lives in the
// search package.
type Elasticsearch struct {
	client  *elasticsearch.Client
	timeout time.Duration
}

func NewElasticsearch(cfg config.ElasticsearchConfig) (*Elasticsearch, error) {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: cfg.RequestTimeout}).DialContext,
		ResponseHeaderTimeout: cfg.RequestTimeout,
		TLSHandshakeTimeout:   cfg.RequestTimeout,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
	}
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses:         []string{cfg.URL},
		Transport:         transport,
		DisableRetry:      true,
		AutoDrainBody:     true,
		DisableMetaHeader: true,
	})
	if err != nil {
		return nil, err
	}
	return &Elasticsearch{client: client, timeout: cfg.RequestTimeout}, nil
}

func (client *Elasticsearch) Perform(ctx context.Context, request *http.Request) (*http.Response, error) {
	if client == nil || client.client == nil || request == nil {
		return nil, errors.New("elasticsearch client is unavailable")
	}
	operationContext, cancel := context.WithTimeout(ctx, client.timeout)
	response, err := client.client.Perform(request.WithContext(operationContext))
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &cancelReadCloser{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

func (client *Elasticsearch) Check(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "/_cluster/health?wait_for_status=yellow&timeout=1s", nil)
	if err != nil {
		return err
	}
	response, err := client.Perform(ctx, request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("elasticsearch health check failed")
	}
	return nil
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (body *cancelReadCloser) Close() error {
	err := body.ReadCloser.Close()
	body.cancel()
	return err
}
