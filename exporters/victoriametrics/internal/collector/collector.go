// Package collector maps only the locked VictoriaMetrics v1.151.0 runtime
// snapshot. It never queries or re-exports the time series stored in the target.
package collector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
)

var ErrUnavailable = errors.New("target_unavailable")

const Unavailable = "# TYPE gopulse_victoriametrics_up gauge\ngopulse_victoriametrics_up 0\n"
const MaxBody = 1 << 20

type Client struct {
	HTTP                       *http.Client
	Origin, Username, Password string
}

func (c *Client) CloseIdleConnections() { c.HTTP.CloseIdleConnections() }

type mapping struct {
	name, kind, upstream, label string
	values                      []string
}

var mappings = []mapping{
	{"rows_inserted_total", "counter", "vm_rows_inserted_total", "type", []string{"csvimport", "datadogsketches", "datadogv1", "datadogv2", "graphite", "influx", "native", "newrelic", "opentelemetry", "opentsdb", "opentsdbhttp", "prometheus", "promremotewrite", "promscrape", "vmimport", "zabbixconnector"}},
	{"query_requests_total", "counter", "vm_http_requests_total", "path", []string{"/api/v1/query", "/api/v1/query_range"}},
	{"active_timeseries", "gauge", "vm_cache_entries", "type", []string{"storage/hour_metric_ids"}},
	{"storage_rows", "gauge", "vm_rows", "type", []string{"storage/inmemory", "storage/small", "storage/big"}},
	{"storage_size_bytes", "gauge", "vm_data_size_bytes", "type", []string{"storage/inmemory", "storage/small", "storage/big", "storage/metaindex", "indexdb/inmemory", "indexdb/file", "indexdb/metaindex"}},
	{"free_disk_space_bytes", "gauge", "vm_free_disk_space_bytes", "path", []string{"/victoria-metrics-data"}},
	{"active_merges", "gauge", "vm_active_merges", "type", []string{"storage/inmemory", "storage/small", "storage/big", "indexdb/inmemory", "indexdb/file"}},
	{"storage_rows_deleted_total", "counter", "vm_rows_deleted_total", "type", []string{"storage/inmemory", "storage/small", "storage/big"}},
}

func Collect(ctx context.Context, c *Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Origin+"/metrics", nil)
	if err != nil {
		return "", ErrUnavailable
	}
	req.SetBasicAuth(c.Username, c.Password)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Encoding") != "" {
		return "", ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxBody+1))
	if err != nil || len(body) > MaxBody {
		return "", ErrUnavailable
	}
	return Snapshot(string(body))
}

// Snapshot selects exact single-label samples. Unknown upstream metrics (including
// gopulse_*), protocols and paths are not output or aggregated. Every selected
// sample must occur once, with a finite nonnegative value and no extra labels.
func Snapshot(body string) (string, error) {
	if len(body) > MaxBody {
		return "", ErrUnavailable
	}
	selected := map[string]int{}
	for i, m := range mappings {
		for _, v := range m.values {
			selected[m.upstream+"{"+m.label+"="+strconv.Quote(v)+"}"] = i
		}
	}
	seen := map[string]bool{}
	sums := make([]float64, len(mappings))
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// All selected names and label values contain no whitespace. A timestamp,
		// duplicate or invalid number is rejected, not a partially usable snapshot.
		fields := strings.Fields(line)
		i, ok := selected[fields[0]]
		if !ok {
			continue
		}
		if len(fields) != 2 || seen[fields[0]] {
			return "", ErrUnavailable
		}
		v, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return "", ErrUnavailable
		}
		sums[i] += v
		if math.IsInf(sums[i], 0) {
			return "", ErrUnavailable
		}
		seen[fields[0]] = true
	}
	if len(seen) != len(selected) {
		return "", ErrUnavailable
	}
	var out strings.Builder
	out.WriteString("# TYPE gopulse_victoriametrics_up gauge\ngopulse_victoriametrics_up 1\n")
	for i, m := range mappings {
		fmt.Fprintf(&out, "# TYPE gopulse_victoriametrics_%s %s\ngopulse_victoriametrics_%s %s\n", m.name, m.kind, m.name, strconv.FormatFloat(sums[i], 'g', -1, 64))
	}
	return out.String(), nil
}
