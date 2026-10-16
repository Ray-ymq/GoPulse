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
	"sort"
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
	required                    bool
	preserveLabel               bool
}

var mappings = []mapping{
	{"rows_inserted_total", "counter", "vm_rows_inserted_total", "type", []string{"csvimport", "datadogsketches", "datadogv1", "datadogv2", "graphite", "influx", "native", "newrelic", "opentelemetry", "opentsdb", "opentsdbhttp", "prometheus", "promremotewrite", "promscrape", "vmimport", "zabbixconnector"}, true, false},
	{"query_requests_total", "counter", "vm_http_requests_total", "path", []string{"/api/v1/query", "/api/v1/query_range"}, true, false},
	{"active_timeseries", "gauge", "vm_cache_entries", "type", []string{"storage/hour_metric_ids"}, true, false},
	{"storage_rows", "gauge", "vm_rows", "type", []string{"storage/inmemory", "storage/small", "storage/big"}, true, false},
	{"storage_size_bytes", "gauge", "vm_data_size_bytes", "type", []string{"storage/inmemory", "storage/small", "storage/big", "storage/metaindex", "indexdb/inmemory", "indexdb/file", "indexdb/metaindex"}, true, false},
	{"free_disk_space_bytes", "gauge", "vm_free_disk_space_bytes", "path", []string{"/victoria-metrics-data"}, true, false},
	{"active_merges", "gauge", "vm_active_merges", "type", []string{"storage/inmemory", "storage/small", "storage/big", "indexdb/inmemory", "indexdb/file"}, true, false},
	{"storage_rows_deleted_total", "counter", "vm_rows_deleted_total", "type", []string{"storage/inmemory", "storage/small", "storage/big"}, true, false},
}

var optionalMappings = []mapping{
	{"rows_ignored_total", "counter", "vm_rows_ignored_total", "reason", []string{"big_timestamp", "big_label_name", "big_label_value", "too_many_labels", "duplicate_label", "bad_value", "timestamp_too_old", "timestamp_too_new"}, false, true},
	{"slow_row_inserts_total", "counter", "vm_slow_row_inserts_total", "", []string{""}, false, false},
	{"cache_size_bytes", "gauge", "vm_cache_size_bytes", "type", []string{"indexdb/indexBlocks", "indexdb/tagFiltersToMetricIDs", "indexdb/tagFiltersLoops", "storage/hour_metric_ids", "storage/indexBlocks", "storage/tsid", "storage/metricIDs", "storage/metricName", "storage/regexps", "storage/regexpPrefixes", "promql/rollupResult"}, false, true},
	{"cache_size_max_bytes", "gauge", "vm_cache_size_max_bytes", "type", []string{"indexdb/indexBlocks", "indexdb/tagFiltersToMetricIDs", "indexdb/tagFiltersLoops", "storage/hour_metric_ids", "storage/indexBlocks", "storage/tsid", "storage/metricIDs", "storage/metricName", "storage/regexps", "storage/regexpPrefixes", "promql/rollupResult"}, false, true},
	{"cache_requests_total", "counter", "vm_cache_requests_total", "type", []string{"indexdb/indexBlocks", "indexdb/tagFiltersToMetricIDs", "indexdb/tagFiltersLoops", "storage/hour_metric_ids", "storage/indexBlocks", "storage/tsid", "storage/metricIDs", "storage/metricName", "storage/regexps", "storage/regexpPrefixes", "promql/rollupResult"}, false, true},
	{"cache_misses_total", "counter", "vm_cache_misses_total", "type", []string{"indexdb/indexBlocks", "indexdb/tagFiltersToMetricIDs", "indexdb/tagFiltersLoops", "storage/hour_metric_ids", "storage/indexBlocks", "storage/tsid", "storage/metricIDs", "storage/metricName", "storage/regexps", "storage/regexpPrefixes", "promql/rollupResult"}, false, true},
	{"cache_entries", "gauge", "vm_cache_entries", "type", []string{"indexdb/indexBlocks", "indexdb/tagFiltersToMetricIDs", "indexdb/tagFiltersLoops", "storage/indexBlocks", "storage/tsid", "storage/metricIDs", "storage/metricName", "storage/regexps", "storage/regexpPrefixes", "promql/rollupResult"}, false, true},
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
	allMappings := append(append([]mapping(nil), mappings...), optionalMappings...)
	selected := map[string]int{}
	for i, m := range allMappings {
		for _, v := range m.values {
			key := m.upstream
			if m.label != "" {
				key += "{" + m.label + "=" + strconv.Quote(v) + "}"
			}
			if _, exists := selected[key]; exists {
				return "", ErrUnavailable
			}
			selected[key] = i
		}
	}
	seen := map[string]bool{}
	sums := make([]float64, len(allMappings))
	values := make(map[string]float64, len(selected))
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
		values[fields[0]] = v
	}
	for i, m := range allMappings {
		if !m.required {
			continue
		}
		for _, value := range m.values {
			key := m.upstream
			if m.label != "" {
				key += "{" + m.label + "=" + strconv.Quote(value) + "}"
			}
			if !seen[key] {
				return "", ErrUnavailable
			}
		}
		_ = i
	}
	var out strings.Builder
	out.WriteString("# TYPE gopulse_victoriametrics_up gauge\ngopulse_victoriametrics_up 1\n")
	typeNames := map[string]string{}
	for _, m := range allMappings {
		if m.required || hasMappingValue(m, values) {
			typeNames["gopulse_victoriametrics_"+m.name] = m.kind
		}
	}
	names := make([]string, 0, len(typeNames))
	for name := range typeNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&out, "# TYPE %s %s\n", name, typeNames[name])
	}
	for i, m := range allMappings {
		metricName := "gopulse_victoriametrics_" + m.name
		if m.preserveLabel {
			for _, value := range m.values {
				key := m.upstream + "{" + m.label + "=" + strconv.Quote(value) + "}"
				if sample, ok := values[key]; ok {
					fmt.Fprintf(&out, "%s{%s=%s} %s\n", metricName, m.label, strconv.Quote(value), strconv.FormatFloat(sample, 'g', -1, 64))
				}
			}
			continue
		}
		if m.required || hasMappingValue(m, values) {
			fmt.Fprintf(&out, "%s %s\n", metricName, strconv.FormatFloat(sums[i], 'g', -1, 64))
		}
	}
	return out.String(), nil
}

func hasMappingValue(m mapping, values map[string]float64) bool {
	for _, value := range m.values {
		key := m.upstream
		if m.label != "" {
			key += "{" + m.label + "=" + strconv.Quote(value) + "}"
		}
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}
