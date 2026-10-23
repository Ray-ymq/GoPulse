// Package telemetrydelivery exposes the delivery state of the two components
// that own the observability hand-off. It intentionally has no dependency on
// storage clients: the admin page reads already-maintained local state only.
package telemetrydelivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/runtimekit"
)

type Queue struct {
	Kind          string `json:"kind"`
	Pending       int64  `json:"pending"`
	OccupiedBytes int64  `json:"occupied_bytes"`
	CapacityBytes int64  `json:"capacity_bytes"`
	FileBytes     int64  `json:"file_bytes"`
	Enqueued      uint64 `json:"enqueued"`
	Sent          uint64 `json:"sent"`
	Failed        uint64 `json:"failed"`
	Evicted       uint64 `json:"evicted"`
	WriteBlocked  bool   `json:"write_blocked"`
	ReasonCode    string `json:"reason_code"`
}

type Item struct {
	Component    string     `json:"component"`
	Instance     string     `json:"instance"`
	Status       string     `json:"status"`
	ObservedAt   *time.Time `json:"observed_at"`
	ReasonCode   string     `json:"reason_code"`
	Queues       []Queue    `json:"queues"`
	WriteBlocked bool       `json:"write_blocked"`
	Discarded    uint64     `json:"discarded"`
}

type Snapshot struct {
	GeneratedAt time.Time `json:"generated_at"`
	Status      string    `json:"status"`
	Items       []Item    `json:"items"`
}

type Source interface {
	Identity() (component, instance string)
	Fetch(context.Context) (Item, error)
}

type Service struct {
	sources []Source
	now     func() time.Time
}

func New(sources []Source) *Service {
	return &Service{sources: append([]Source(nil), sources...), now: time.Now}
}

func (s *Service) Snapshot(ctx context.Context) Snapshot {
	if ctx == nil {
		ctx = context.Background()
	}
	result := Snapshot{GeneratedAt: s.now().UTC(), Status: "healthy", Items: make([]Item, len(s.sources))}
	failed := make([]bool, len(s.sources))
	var wg sync.WaitGroup
	for index, source := range s.sources {
		index, source := index, source
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := source.Fetch(ctx)
			if err == nil {
				result.Items[index] = item
				failed[index] = item.Status != "healthy"
				return
			}
			component, instance := source.Identity()
			result.Items[index] = Item{Component: component, Instance: instance, Status: "unknown", ReasonCode: "upstream_unavailable", Queues: []Queue{}}
			failed[index] = true
		}()
	}
	wg.Wait()
	for _, itemFailed := range failed {
		if itemFailed {
			result.Status = "degraded"
			break
		}
	}
	if len(result.Items) == 0 {
		result.Status = "unknown"
	}
	return result
}

type HTTPSource struct {
	component string
	instance  string
	url       string
	token     string
	client    *http.Client
}

func NewHTTPSource(component, instance, rawURL, token string, timeout time.Duration) (*HTTPSource, error) {
	if component == "" || instance == "" {
		return nil, errors.New("telemetry delivery source identity is required")
	}
	parsed, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("telemetry delivery source URL is invalid")
	}
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	return &HTTPSource{component: component, instance: instance, url: parsed.String() + "/internal/v1/telemetry-delivery", token: token, client: &http.Client{Timeout: timeout}}, nil
}

func (s *HTTPSource) Identity() (string, string) { return s.component, s.instance }

func (s *HTTPSource) Fetch(ctx context.Context) (Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return Item{}, err
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	response, err := s.client.Do(req)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Item{}, fmt.Errorf("delivery source returned HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Data Item `json:"data"`
	}
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return Item{}, err
	}
	item := envelope.Data
	// The configured source identity is the service address used for the
	// request. Compose deliberately keeps that address (for example,
	// "marshaller") separate from the process identity returned by the remote
	// service (for example, "marshaller-1"). Keep the latter in the response so
	// the dashboard reports the actual process, while using the configured
	// address only as the fallback identity when the source is unavailable.
	if item.Component != s.component || runtimekit.ValidateInstanceID(item.Instance) != nil || item.ObservedAt == nil {
		return Item{}, errors.New("delivery source returned an invalid status")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Item{}, errors.New("delivery source returned an invalid status")
	}
	if item.Status == "" {
		item.Status, item.ReasonCode = "healthy", "ok"
		for _, queue := range item.Queues {
			if queue.WriteBlocked {
				item.Status, item.ReasonCode = "degraded", queue.ReasonCode
				if item.ReasonCode == "" {
					item.ReasonCode = "write_blocked"
				}
				break
			}
		}
	}
	if item.ReasonCode == "" {
		item.ReasonCode = "ok"
	}
	if item.Status != "healthy" && item.Status != "degraded" && item.Status != "unknown" {
		return Item{}, errors.New("delivery source returned an invalid status value")
	}
	if item.Queues == nil {
		item.Queues = []Queue{}
	}
	return item, nil
}

// EnvironmentSources builds the Monitor source plus one source per configured
// Marshaller instance. Missing credentials are left as an unavailable source
// so a delivery dashboard outage cannot prevent the platform from starting.
func EnvironmentSources(monitorURL, monitorToken, marshallerToken string, timeout time.Duration) ([]Source, error) {
	sources := make([]Source, 0, 3)
	monitor, err := NewHTTPSource("monitor", "monitor", monitorURL, monitorToken, timeout)
	if err != nil {
		return nil, err
	}
	sources = append(sources, monitor)
	mode := runtimekit.Mode()
	endpoints, err := runtimekit.ReplicaEndpoints("marshaller")
	if err != nil {
		return nil, err
	}
	port, err := configuredPort("MARSHALLER_HTTP_PORT", 9093)
	if err != nil {
		return nil, err
	}
	for _, endpoint := range endpoints {
		host := endpoint
		if mode == "host" && (endpoint == "marshaller" || endpoint == "marshaller-local") {
			host = "127.0.0.1"
		}
		source, sourceErr := NewHTTPSource("marshaller", endpoint, "http://"+host+":"+port, marshallerToken, timeout)
		if sourceErr != nil {
			return nil, sourceErr
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func configuredPort(key string, fallback int) (string, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return strconv.Itoa(fallback), nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("%s is invalid", key)
	}
	return strconv.Itoa(port), nil
}
