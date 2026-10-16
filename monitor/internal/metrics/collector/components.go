package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/Ray-ymq/GoPulse/componentmetrics/metriccontract"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/publisher"
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
	type target struct{ id, endpoint, origin, token string }
	targets := make([]target, 0, len(componentmetrics.Components))
	for _, id := range componentmetrics.Components {
		address, err := componentmetrics.Address(mode, id)
		if err != nil {
			return nil, err
		}
		_, port, _ := net.SplitHostPort(address)
		token, err := componentmetrics.Token(id)
		if err != nil {
			return nil, err
		}
		endpoints, err := componentmetrics.ReplicaEndpoints(id)
		if err != nil {
			return nil, err
		}
		for _, endpoint := range endpoints {
			host := "127.0.0.1"
			if mode == "container" {
				host = endpoint
			} else if endpoint != id {
				return nil, errors.New("host component endpoints must use the local component name")
			}
			targets = append(targets, target{id: id, endpoint: endpoint, origin: "http://" + net.JoinHostPort(host, port) + componentmetrics.Path, token: token})
		}
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
				request, err := componentmetrics.NewRequest(ctx, http.MethodGet, t.origin, nil)
				if err != nil {
					return
				}
				request.Header.Set("Authorization", "Bearer "+t.token)
				response, err := client.Do(request)
				var samples []envelope.Sample
				if err == nil {
					body, readErr := io.ReadAll(io.LimitReader(response.Body, metriccontract.MaxBodyBytes+1))
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
				targetID := componentmetrics.TargetFor(t.id, t.endpoint)
				componentmetrics.Active().Observe("scrapes_total", time.Since(started), "component", targetID, result)
				if err != nil {
					if ctx.Err() == nil {
						logger.Warn("component metrics unavailable", "component", t.id, "instance_id", t.endpoint, "reason", "scrape_failed")
					}
					return
				}
				componentmetrics.Active().Set("last_scrape_success_timestamp_seconds", float64(time.Now().Unix()), "component", targetID)
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
				componentmetrics.Active().Observe("scrapes_total", time.Since(started), "component", targetID, result)
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
	if !componentmetrics.IsComponent(id) {
		return nil, errors.New("invalid component")
	}
	return parseExposition(id, body)
}
