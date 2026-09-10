// Package collector reads a bounded, fixed-vhost management snapshot. No target
// URL, API path or object name is accepted from management API callers.
package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var ErrUnavailable = errors.New("target_unavailable")

const Unavailable = "# TYPE gopulse_rabbitmq_up gauge\ngopulse_rabbitmq_up 0\n"

type Client struct {
	HTTP                       *http.Client
	Origin, Username, Password string
}

func (c *Client) CloseIdleConnections() { c.HTTP.CloseIdleConnections() }
func (c *Client) read(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Origin+path, nil)
	if err != nil {
		return ErrUnavailable
	}
	req.SetBasicAuth(c.Username, c.Password)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Encoding") != "" {
		return ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return ErrUnavailable
	}
	if json.Unmarshal(raw, dst) != nil {
		return ErrUnavailable
	}
	return nil
}

type vhost struct {
	Name  string          `json:"name"`
	Stats json.RawMessage `json:"message_stats"`
}
type scoped struct {
	Vhost string `json:"vhost"`
}
type queue struct {
	Vhost     string  `json:"vhost"`
	Consumers *uint64 `json:"consumers"`
	Ready     *uint64 `json:"messages_ready"`
	Unacked   *uint64 `json:"messages_unacknowledged"`
}

func Collect(ctx context.Context, c *Client) (string, error) {
	var vhosts []vhost
	if c.read(ctx, "/api/vhosts", &vhosts) != nil || vhosts == nil {
		return "", ErrUnavailable
	}
	var target *vhost
	for i := range vhosts {
		if vhosts[i].Name == "/" {
			if target != nil {
				return "", ErrUnavailable
			}
			target = &vhosts[i]
		}
	}
	if target == nil {
		return "", ErrUnavailable
	}
	var connections, channels []scoped
	var queues []queue
	if c.read(ctx, "/api/vhosts/%2F/connections", &connections) != nil || connections == nil || c.read(ctx, "/api/vhosts/%2F/channels", &channels) != nil || channels == nil || c.read(ctx, "/api/queues/%2F", &queues) != nil || queues == nil {
		return "", ErrUnavailable
	}
	for _, items := range [][]scoped{connections, channels} {
		for _, item := range items {
			if item.Vhost != "/" {
				return "", ErrUnavailable
			}
		}
	}
	values := map[string]uint64{"connections": uint64(len(connections)), "channels": uint64(len(channels)), "queues": uint64(len(queues))}
	for _, q := range queues {
		if q.Vhost != "/" || q.Consumers == nil || q.Ready == nil || q.Unacked == nil {
			return "", ErrUnavailable
		}
		for key, n := range map[string]uint64{"consumers": *q.Consumers, "ready": *q.Ready, "unacked": *q.Unacked} {
			if ^uint64(0)-values[key] < n {
				return "", ErrUnavailable
			}
			values[key] += n
		}
	}
	// RabbitMQ 3.13.3 documents that message_stats fields only appear after
	// activity. This exception is limited to the three counters, never gauges.
	stats := map[string]json.RawMessage{}
	if len(target.Stats) > 0 {
		if string(target.Stats) == "null" || json.Unmarshal(target.Stats, &stats) != nil {
			return "", ErrUnavailable
		}
	}
	for name, key := range map[string]string{"published_total": "publish", "delivered_total": "deliver_get", "acked_total": "ack"} {
		var n uint64
		if raw, ok := stats[key]; ok {
			if string(raw) == "null" || json.Unmarshal(raw, &n) != nil {
				return "", ErrUnavailable
			}
		}
		values[name] = n
	}
	var b strings.Builder
	b.WriteString("# TYPE gopulse_rabbitmq_up gauge\ngopulse_rabbitmq_up 1\n")
	for _, name := range []string{"connections", "channels", "queues", "consumers", "messages", "published_total", "delivered_total", "acked_total"} {
		kind := "gauge"
		if strings.HasSuffix(name, "_total") {
			kind = "counter"
		}
		fmt.Fprintf(&b, "# TYPE gopulse_rabbitmq_%s %s\n", name, kind)
		if name == "messages" {
			fmt.Fprintf(&b, "gopulse_rabbitmq_messages{state=\"ready\"} %d\ngopulse_rabbitmq_messages{state=\"unacked\"} %d\n", values["ready"], values["unacked"])
		} else {
			fmt.Fprintf(&b, "gopulse_rabbitmq_%s %d\n", name, values[name])
		}
	}
	return b.String(), nil
}
