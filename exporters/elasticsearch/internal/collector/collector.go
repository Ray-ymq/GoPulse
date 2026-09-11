// Package collector reads fixed, read-only cluster and primary-index statistics.
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

const Unavailable = "# TYPE gopulse_elasticsearch_up gauge\ngopulse_elasticsearch_up 0\n"

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
	if c.Username != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
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

// Pointers distinguish a real zero from an incomplete upstream snapshot.
type health struct {
	Status       string  `json:"status"`
	TimedOut     *bool   `json:"timed_out"`
	Nodes        *uint64 `json:"number_of_nodes"`
	DataNodes    *uint64 `json:"number_of_data_nodes"`
	Primary      *uint64 `json:"active_primary_shards"`
	Active       *uint64 `json:"active_shards"`
	Relocating   *uint64 `json:"relocating_shards"`
	Initializing *uint64 `json:"initializing_shards"`
	Unassigned   *uint64 `json:"unassigned_shards"`
	Pending      *uint64 `json:"number_of_pending_tasks"`
}
type stats struct {
	Shards struct{ Total, Successful, Failed *uint64 } `json:"_shards"`
	All    struct {
		Primaries struct {
			Docs struct {
				Count *uint64 `json:"count"`
			} `json:"docs"`
			Store struct {
				Bytes *uint64 `json:"size_in_bytes"`
			} `json:"store"`
		} `json:"primaries"`
	} `json:"_all"`
}

func Collect(ctx context.Context, c *Client) (string, error) {
	var h health
	var s stats
	if c.read(ctx, "/_cluster/health", &h) != nil || c.read(ctx, "/_stats/docs,store?level=cluster", &s) != nil {
		return "", ErrUnavailable
	}
	if h.TimedOut == nil || *h.TimedOut || (h.Status != "green" && h.Status != "yellow" && h.Status != "red") || s.Shards.Total == nil || s.Shards.Successful == nil || s.Shards.Failed == nil || *s.Shards.Failed != 0 {
		return "", ErrUnavailable
	}
	names := []string{"nodes", "data_nodes", "active_primary_shards", "active_shards", "relocating_shards", "initializing_shards", "unassigned_shards", "pending_tasks", "documents", "store_size_bytes"}
	values := []*uint64{h.Nodes, h.DataNodes, h.Primary, h.Active, h.Relocating, h.Initializing, h.Unassigned, h.Pending, s.All.Primaries.Docs.Count, s.All.Primaries.Store.Bytes}
	var b strings.Builder
	b.WriteString("# TYPE gopulse_elasticsearch_up gauge\ngopulse_elasticsearch_up 1\n# TYPE gopulse_elasticsearch_cluster_health_status gauge\n")
	for _, status := range []string{"green", "yellow", "red"} {
		value := 0
		if h.Status == status {
			value = 1
		}
		fmt.Fprintf(&b, "gopulse_elasticsearch_cluster_health_status{status=%q} %d\n", status, value)
	}
	for i, name := range names {
		if values[i] == nil {
			return "", ErrUnavailable
		}
		fmt.Fprintf(&b, "# TYPE gopulse_elasticsearch_%s gauge\ngopulse_elasticsearch_%s %d\n", name, name, *values[i])
	}
	return b.String(), nil
}
