package collector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/publisher"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// Components is independent of the plugin registry and lifecycle API.
type Components struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func StartComponents(root context.Context, mode, version string, interval, timeout, publishTimeout time.Duration, p publisher.Publisher, logger *slog.Logger) (*Components, error) {
	if interval <= 0 || timeout <= 0 || timeout >= interval || !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(version) {
		return nil, errors.New("invalid component scrape configuration")
	}
	type target struct{ id, origin, token string }
	targets := make([]target, 0, 6)
	for _, id := range componentmetrics.Components {
		address, err := componentmetrics.Address(mode, id)
		if err != nil {
			return nil, err
		}
		if mode == "container" {
			_, port, _ := net.SplitHostPort(address)
			address = net.JoinHostPort(id, port)
		}
		token, err := componentmetrics.Token(id)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target{id, "http://" + address + componentmetrics.Path, token})
	}
	ctx, cancel := context.WithCancel(root)
	c := &Components{cancel: cancel, done: make(chan struct{})}
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			transport.DisableCompression = true
			transport.ResponseHeaderTimeout = timeout
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			scrape := func() {
				started := time.Now()
				spec, _ := componentmetrics.Catalog(t.id)
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, t.origin, nil)
				if err != nil {
					return
				}
				request.Header.Set("Authorization", "Bearer "+t.token)
				response, err := client.Do(request)
				var samples []envelope.Sample
				if err == nil {
					body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(spec.MaxBodyBytes)+1))
					_ = response.Body.Close()
					mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
					if len(response.Header.Values("Content-Type")) != 1 || mediaErr != nil || mediaType != "text/plain" || response.Header.Get("Content-Encoding") != "" {
						readErr = errors.New("invalid component response type")
					}
					if readErr != nil {
						err = readErr
					} else if response.StatusCode != http.StatusOK {
						err = errors.New("component endpoint unavailable")
					} else {
						samples, err = ParseComponent(t.id, body)
					}
				}
				result := "scrape_success"
				if err != nil {
					result = "scrape_failure"
				}
				componentmetrics.Active().Observe("scrapes_total", time.Since(started), "component", componentmetrics.Target(t.id), result)
				if err != nil {
					if ctx.Err() == nil {
						logger.Warn("component metrics unavailable", "component", t.id, "reason", "scrape_failed")
					}
					return
				}
				componentmetrics.Active().Set("last_scrape_success_timestamp_seconds", float64(time.Now().Unix()), "component", componentmetrics.Target(t.id))
				message, err := envelope.NewComponent(t.id, version, samples, time.Now())
				if err != nil {
					return
				}
				started = time.Now()
				publishCtx, publishCancel := context.WithTimeout(ctx, publishTimeout)
				err = p.Publish(publishCtx, message)
				publishCancel()
				result = "publish_success"
				if err != nil {
					result = "publish_failure"
				}
				componentmetrics.Active().Observe("scrapes_total", time.Since(started), "component", componentmetrics.Target(t.id), result)
			}
			scrape()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					scrape()
				}
			}
		}()
	}
	go func() { wg.Wait(); close(c.done) }()
	return c, nil
}
func (c *Components) Shutdown(ctx context.Context) error {
	c.cancel()
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ParseComponent(id string, body []byte) ([]envelope.Sample, error) {
	spec, ok := componentmetrics.Catalog(id)
	if !ok || len(body) > spec.MaxBodyBytes || bytes.ContainsRune(body, 0) {
		return nil, errors.New("invalid component body")
	}
	allowed := make(map[string]componentmetrics.Family, len(spec.Families))
	for _, f := range spec.Families {
		allowed[f.Name] = f
	}
	// TYPE-only families may be omitted by the text parser when no tuple exists.
	// Still reject unknown declarations rather than letting empty extras through.
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "#" && (fields[1] == "TYPE" || fields[1] == "HELP") {
			if _, ok := allowed[fields[2]]; !ok {
				return nil, errors.New("unknown component family")
			}
		}
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid component exposition")
	}
	var checked []componentmetrics.Sample
	for name, family := range families {
		f, ok := allowed[name]
		if !ok {
			return nil, errors.New("unknown component family")
		}
		for _, metric := range family.Metric {
			if len(checked) >= spec.MaxSamples || metric.TimestampMs != nil {
				return nil, errors.New("invalid component sample")
			}
			labels := make(map[string]string, len(metric.Label))
			for _, label := range metric.Label {
				key := label.GetName()
				if _, duplicate := labels[key]; duplicate {
					return nil, errors.New("duplicate component label")
				}
				labels[key] = label.GetValue()
			}
			var value float64
			if f.Kind == "counter" && family.GetType() == dto.MetricType_COUNTER && metric.Counter != nil {
				value = metric.Counter.GetValue()
			} else if f.Kind == "gauge" && family.GetType() == dto.MetricType_GAUGE && metric.Gauge != nil {
				value = metric.Gauge.GetValue()
			} else {
				return nil, errors.New("invalid component kind")
			}
			checked = append(checked, componentmetrics.Sample{Name: name, Kind: f.Kind, Labels: labels, Value: value})
		}
	}
	if err := componentmetrics.Validate(id, checked); err != nil {
		return nil, err
	}
	samples := make([]envelope.Sample, len(checked))
	for i, s := range checked {
		samples[i] = envelope.Sample{Name: s.Name, Kind: s.Kind, Labels: s.Labels, Value: s.Value}
	}
	return samples, nil
}
