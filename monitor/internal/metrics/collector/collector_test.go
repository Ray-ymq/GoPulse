package collector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

const successText = `# HELP gopulse_redis_up Whether the current Redis scrape succeeded.
# TYPE gopulse_redis_up gauge
gopulse_redis_up 1
# TYPE gopulse_redis_uptime_seconds gauge
gopulse_redis_uptime_seconds 10
# TYPE gopulse_redis_connected_clients gauge
gopulse_redis_connected_clients 2
# TYPE gopulse_redis_used_memory_bytes gauge
gopulse_redis_used_memory_bytes 100
# TYPE gopulse_redis_commands_processed_total counter
gopulse_redis_commands_processed_total 7
# TYPE gopulse_redis_keyspace_hits_total counter
gopulse_redis_keyspace_hits_total 3
# TYPE gopulse_redis_keyspace_misses_total counter
gopulse_redis_keyspace_misses_total 1
# TYPE gopulse_redis_cpu_seconds_total counter
gopulse_redis_cpu_seconds_total{mode="user"} 1.5
gopulse_redis_cpu_seconds_total{mode="system"} 0.5
# TYPE gopulse_redis_db_keys gauge
gopulse_redis_db_keys{db="0"} 4
# TYPE gopulse_redis_db_expiring_keys gauge
gopulse_redis_db_expiring_keys{db="0"} 1
`

type capturePublisher struct{ messages chan envelope.Envelope }

func (p capturePublisher) Publish(_ context.Context, value envelope.Envelope) error {
	p.messages <- value
	return nil
}

func monitorForServer(t *testing.T, server *httptest.Server, pub capturePublisher, update func(Update)) *Monitor {
	t.Helper()
	parsed, _ := url.Parse(server.URL)
	_, port, _ := strings.Cut(parsed.Host, ":")
	m, err := New(Config{Host: "127.0.0.1", Port: port, Interval: time.Second, Timeout: 500 * time.Millisecond, PublishTimeout: 500 * time.Millisecond, Publisher: pub, Update: update})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMonitorPublishesSuccessAndUnavailable(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body, want string
		samples    int
	}{{"success", 200, successText, "success", 11}, {"unavailable", 503, "# TYPE gopulse_redis_up gauge\ngopulse_redis_up 0\n", "target_unavailable", 1}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			pub := capturePublisher{make(chan envelope.Envelope, 1)}
			updates := make(chan Update, 1)
			m := monitorForServer(t, server, pub, func(u Update) { updates <- u })
			m.Enable(plugin.Manifest{ID: plugin.PluginID, Version: "1.3.3", MetricsPath: "/metrics"})
			defer m.Shutdown(context.Background())
			select {
			case message := <-pub.messages:
				if message.Payload.ScrapeStatus != tc.want || len(message.Payload.Samples) != tc.samples || len(message.MessageID) != 32 || message.Payload.TargetID != envelope.TargetID {
					t.Fatalf("unexpected message: %#v", message)
				}
			case <-time.After(time.Second):
				t.Fatal("message was not published")
			}
			select {
			case u := <-updates:
				if u.ScrapeAt == nil || (tc.want == "success") != (u.SuccessAt != nil) {
					t.Fatalf("unexpected update: %#v", u)
				}
			case <-time.After(time.Second):
				t.Fatal("status was not updated")
			}
		})
	}
}

func TestMonitorRejectsMalformedAndOversizedResponses(t *testing.T) {
	for _, body := range []string{"# TYPE gopulse_redis_up gauge\ngopulse_redis_up NaN\n", strings.Repeat("x", int(MaxResponseBytes)+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(body))
		}))
		pub := capturePublisher{make(chan envelope.Envelope, 1)}
		updates := make(chan Update, 1)
		m := monitorForServer(t, server, pub, func(u Update) { updates <- u })
		m.Enable(plugin.Manifest{ID: plugin.PluginID, Version: "1.3.3", MetricsPath: "/metrics"})
		select {
		case <-pub.messages:
			t.Fatal("invalid response was published")
		case u := <-updates:
			if u.ErrorCode == "" {
				t.Fatal("missing safe error")
			}
		case <-time.After(time.Second):
			t.Fatal("invalid response was not recorded")
		}
		_ = m.Shutdown(context.Background())
		server.Close()
	}
}

type manualTicker struct{ ch chan time.Time }

func (t *manualTicker) Chan() <-chan time.Time { return t.ch }
func (*manualTicker) Stop()                    {}
func TestMonitorDoesNotOverlapScrapes(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{}, 2)
	var active, max atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		if current > max.Load() {
			max.Store(current)
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(successText))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	_, portText, _ := strings.Cut(parsed.Host, ":")
	if _, err := strconv.Atoi(portText); err != nil {
		t.Fatal(err)
	}
	ticker := &manualTicker{make(chan time.Time, 1)}
	pub := capturePublisher{make(chan envelope.Envelope, 2)}
	m, err := New(Config{Host: "127.0.0.1", Port: portText, Interval: 2 * time.Second, Timeout: time.Second, PublishTimeout: time.Second, Publisher: pub, NewTicker: func(time.Duration) Ticker { return ticker }})
	if err != nil {
		t.Fatal(err)
	}
	m.Enable(plugin.Manifest{ID: plugin.PluginID, Version: "1.3.3", MetricsPath: "/metrics"})
	<-entered
	ticker.ch <- time.Now()
	select {
	case <-entered:
		t.Fatal("second scrape overlapped")
	case <-time.After(50 * time.Millisecond):
	}
	release <- struct{}{}
	<-entered
	release <- struct{}{}
	if err = m.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if max.Load() != 1 {
		t.Fatalf("max concurrent scrapes=%d", max.Load())
	}
}

type delayedCancelPublisher struct {
	started chan struct{}
	release chan struct{}
}

func (p delayedCancelPublisher) Publish(ctx context.Context, _ envelope.Envelope) error {
	close(p.started)
	<-ctx.Done()
	<-p.release
	return ctx.Err()
}

func TestDisableRetainsGenerationUntilItCanBeJoined(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(successText))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	_, port, _ := strings.Cut(parsed.Host, ":")
	publisher := delayedCancelPublisher{started: make(chan struct{}), release: make(chan struct{})}
	monitor, err := New(Config{Host: "127.0.0.1", Port: port, Interval: time.Second, Timeout: 500 * time.Millisecond, PublishTimeout: time.Second, Publisher: publisher})
	if err != nil {
		t.Fatal(err)
	}
	monitor.Enable(plugin.Manifest{ID: plugin.PluginID, Version: "1.3.3", MetricsPath: "/metrics"})
	<-publisher.started
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err = monitor.Disable(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("first Disable() error = %v, want context canceled", err)
	}
	joined := make(chan error, 1)
	go func() { joined <- monitor.Disable(context.Background()) }()
	select {
	case err = <-joined:
		t.Fatalf("second Disable returned before generation ended: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(publisher.release)
	if err = <-joined; err != nil {
		t.Fatalf("second Disable() error = %v", err)
	}
}
