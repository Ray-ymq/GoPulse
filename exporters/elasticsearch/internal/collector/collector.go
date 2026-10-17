// Package collector reads fixed, read-only cluster and primary-index statistics.
package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
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

type nodeStatsResponse struct {
	Nodes map[string]nodeStats `json:"nodes"`
}

type nodeStats struct {
	JVM struct {
		Mem struct {
			HeapUsed *uint64 `json:"heap_used_in_bytes"`
			HeapMax  *uint64 `json:"heap_max_in_bytes"`
		} `json:"mem"`
		GC struct {
			Collectors map[string]gcStats `json:"collectors"`
		} `json:"gc"`
	} `json:"jvm"`
	ThreadPool map[string]threadPoolStats `json:"thread_pool"`
	Indices    struct {
		Search       searchStats    `json:"search"`
		Indexing     indexingStats  `json:"indexing"`
		QueryCache   cacheStats     `json:"query_cache"`
		RequestCache cacheStats     `json:"request_cache"`
		Fielddata    fielddataStats `json:"fielddata"`
		Merges       mergeStats     `json:"merges"`
		Refresh      refreshStats   `json:"refresh"`
		Flush        flushStats     `json:"flush"`
		Segments     segmentsStats  `json:"segments"`
	} `json:"indices"`
	Breakers map[string]breakerStats `json:"breakers"`
	FS       struct {
		Total struct {
			Total     *uint64 `json:"total_in_bytes"`
			Free      *uint64 `json:"free_in_bytes"`
			Available *uint64 `json:"available_in_bytes"`
		} `json:"total"`
	} `json:"fs"`
}

type gcStats struct {
	CollectionCount *uint64 `json:"collection_count"`
	CollectionTime  *uint64 `json:"collection_time_in_millis"`
}

type threadPoolStats struct {
	Threads   *uint64 `json:"threads"`
	Queue     *uint64 `json:"queue"`
	Active    *uint64 `json:"active"`
	Rejected  *uint64 `json:"rejected"`
	Completed *uint64 `json:"completed"`
}

type searchStats struct {
	QueryTotal      *uint64 `json:"query_total"`
	QueryTimeMillis *uint64 `json:"query_time_in_millis"`
	QueryCurrent    *uint64 `json:"query_current"`
	FetchTotal      *uint64 `json:"fetch_total"`
	FetchTimeMillis *uint64 `json:"fetch_time_in_millis"`
	FetchCurrent    *uint64 `json:"fetch_current"`
}

type indexingStats struct {
	IndexTotal      *uint64 `json:"index_total"`
	IndexTimeMillis *uint64 `json:"index_time_in_millis"`
	IndexCurrent    *uint64 `json:"index_current"`
	IndexFailed     *uint64 `json:"index_failed"`
}

type cacheStats struct {
	MemorySize *uint64 `json:"memory_size_in_bytes"`
	HitCount   *uint64 `json:"hit_count"`
	MissCount  *uint64 `json:"miss_count"`
	Evictions  *uint64 `json:"evictions"`
}

type fielddataStats struct {
	MemorySize *uint64 `json:"memory_size_in_bytes"`
}

type mergeStats struct {
	Current     *uint64 `json:"current"`
	CurrentSize *uint64 `json:"current_size_in_bytes"`
	Total       *uint64 `json:"total"`
	TotalTime   *uint64 `json:"total_time_in_millis"`
}

type refreshStats struct {
	Total     *uint64 `json:"total"`
	TotalTime *uint64 `json:"total_time_in_millis"`
	Listeners *uint64 `json:"listeners"`
}

type flushStats struct {
	Total     *uint64 `json:"total"`
	TotalTime *uint64 `json:"total_time_in_millis"`
}

type segmentsStats struct {
	Count  *uint64 `json:"count"`
	Memory *uint64 `json:"memory_in_bytes"`
}

type breakerStats struct {
	LimitSize     *uint64 `json:"limit_size_in_bytes"`
	EstimatedSize *uint64 `json:"estimated_size_in_bytes"`
	Tripped       *uint64 `json:"tripped"`
}

type sample struct {
	name, kind string
	labels     map[string]string
	value      string
}

var supportedCircuitBreakers = []string{"parent", "fielddata", "request", "in_flight_requests", "accounting", "model_inference"}

func addSample(samples *[]sample, name, kind, value string, labels map[string]string) {
	copyLabels := make(map[string]string, len(labels))
	for key, label := range labels {
		copyLabels[key] = label
	}
	*samples = append(*samples, sample{name: name, kind: kind, labels: copyLabels, value: value})
}

func renderSamples(samples []sample) string {
	types := map[string]string{}
	for _, item := range samples {
		types[item.name] = item.kind
	}
	typeNames := make([]string, 0, len(types))
	for name := range types {
		typeNames = append(typeNames, name)
	}
	sort.Strings(typeNames)
	var b strings.Builder
	for _, name := range typeNames {
		fmt.Fprintf(&b, "# TYPE %s %s\n", name, types[name])
	}
	sort.Slice(samples, func(i, j int) bool {
		if samples[i].name != samples[j].name {
			return samples[i].name < samples[j].name
		}
		return labelKey(samples[i].labels) < labelKey(samples[j].labels)
	})
	for _, item := range samples {
		fmt.Fprintf(&b, "%s%s %s\n", item.name, formatLabels(item.labels), item.value)
	}
	return b.String()
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Quote(labels[key]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func labelKey(labels map[string]string) string {
	return formatLabels(labels)
}

func Collect(ctx context.Context, c *Client) (string, error) {
	var h health
	var s stats
	var ns nodeStatsResponse
	if c.read(ctx, "/_cluster/health", &h) != nil || c.read(ctx, "/_stats/docs,store?level=cluster", &s) != nil || c.read(ctx, "/_nodes/stats/jvm,thread_pool,indices,fs,breakers", &ns) != nil {
		return "", ErrUnavailable
	}
	if h.TimedOut == nil || *h.TimedOut || (h.Status != "green" && h.Status != "yellow" && h.Status != "red") || s.Shards.Total == nil || s.Shards.Successful == nil || s.Shards.Failed == nil || *s.Shards.Failed != 0 || len(ns.Nodes) == 0 || len(ns.Nodes) > 128 {
		return "", ErrUnavailable
	}
	var samples []sample
	addSample(&samples, "gopulse_elasticsearch_up", "gauge", "1", nil)
	for _, status := range []string{"green", "yellow", "red"} {
		value := 0
		if h.Status == status {
			value = 1
		}
		addSample(&samples, "gopulse_elasticsearch_cluster_health_status", "gauge", strconv.Itoa(value), map[string]string{"status": status})
	}
	names := []string{"nodes", "data_nodes", "active_primary_shards", "active_shards", "relocating_shards", "initializing_shards", "unassigned_shards", "pending_tasks", "documents", "store_size_bytes"}
	values := []*uint64{h.Nodes, h.DataNodes, h.Primary, h.Active, h.Relocating, h.Initializing, h.Unassigned, h.Pending, s.All.Primaries.Docs.Count, s.All.Primaries.Store.Bytes}
	for i, name := range names {
		if values[i] == nil {
			return "", ErrUnavailable
		}
		addSample(&samples, "gopulse_elasticsearch_"+name, "gauge", strconv.FormatUint(*values[i], 10), nil)
	}

	threadPools := []string{"search", "write", "get", "management", "refresh", "flush", "force_merge", "snapshot", "generic", "listener"}
	nodeIDs := make([]string, 0, len(ns.Nodes))
	for nodeID := range ns.Nodes {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	for _, nodeID := range nodeIDs {
		node := ns.Nodes[nodeID]
		if !validLabel(nodeID) || node.JVM.Mem.HeapUsed == nil || node.JVM.Mem.HeapMax == nil || node.FS.Total.Total == nil || node.FS.Total.Free == nil || node.FS.Total.Available == nil {
			return "", ErrUnavailable
		}
		labels := map[string]string{"node_id": nodeID}
		addSample(&samples, "gopulse_elasticsearch_jvm_heap_used_bytes", "gauge", strconv.FormatUint(*node.JVM.Mem.HeapUsed, 10), labels)
		addSample(&samples, "gopulse_elasticsearch_jvm_heap_max_bytes", "gauge", strconv.FormatUint(*node.JVM.Mem.HeapMax, 10), labels)
		addSample(&samples, "gopulse_elasticsearch_filesystem_bytes", "gauge", strconv.FormatUint(*node.FS.Total.Total, 10), map[string]string{"node_id": nodeID, "state": "total"})
		addSample(&samples, "gopulse_elasticsearch_filesystem_bytes", "gauge", strconv.FormatUint(*node.FS.Total.Free, 10), map[string]string{"node_id": nodeID, "state": "free"})
		addSample(&samples, "gopulse_elasticsearch_filesystem_bytes", "gauge", strconv.FormatUint(*node.FS.Total.Available, 10), map[string]string{"node_id": nodeID, "state": "available"})
		gcNames := make([]string, 0, len(node.JVM.GC.Collectors))
		for gcName := range node.JVM.GC.Collectors {
			gcNames = append(gcNames, gcName)
		}
		if len(gcNames) > 16 {
			return "", ErrUnavailable
		}
		sort.Strings(gcNames)
		for _, gcName := range gcNames {
			if !validLabel(gcName) {
				return "", ErrUnavailable
			}
			gc := node.JVM.GC.Collectors[gcName]
			gcLabels := map[string]string{"node_id": nodeID, "gc": gcName}
			if gc.CollectionCount != nil {
				addSample(&samples, "gopulse_elasticsearch_jvm_gc_collections_total", "counter", strconv.FormatUint(*gc.CollectionCount, 10), gcLabels)
			}
			if gc.CollectionTime != nil {
				addSample(&samples, "gopulse_elasticsearch_jvm_gc_collection_time_seconds_total", "counter", strconv.FormatFloat(float64(*gc.CollectionTime)/1000, 'g', -1, 64), gcLabels)
			}
		}
		for _, pool := range threadPools {
			stats, ok := node.ThreadPool[pool]
			if !ok {
				continue
			}
			poolLabels := map[string]string{"node_id": nodeID, "thread_pool": pool}
			if stats.Active != nil {
				addSample(&samples, "gopulse_elasticsearch_thread_pool_active", "gauge", strconv.FormatUint(*stats.Active, 10), poolLabels)
			}
			if stats.Queue != nil {
				addSample(&samples, "gopulse_elasticsearch_thread_pool_queue", "gauge", strconv.FormatUint(*stats.Queue, 10), poolLabels)
			}
			if stats.Completed != nil {
				addSample(&samples, "gopulse_elasticsearch_thread_pool_completed_total", "counter", strconv.FormatUint(*stats.Completed, 10), poolLabels)
			}
			if stats.Rejected != nil {
				addSample(&samples, "gopulse_elasticsearch_thread_pool_rejected_total", "counter", strconv.FormatUint(*stats.Rejected, 10), poolLabels)
			}
		}
		search := node.Indices.Search
		for _, metric := range []struct {
			name, kind, operation string
			value                 *uint64
			scale                 float64
		}{
			{"search_operations_total", "counter", "query", search.QueryTotal, 1},
			{"search_duration_seconds_total", "counter", "query", search.QueryTimeMillis, 0.001},
			{"search_in_flight", "gauge", "query", search.QueryCurrent, 1},
			{"search_operations_total", "counter", "fetch", search.FetchTotal, 1},
			{"search_duration_seconds_total", "counter", "fetch", search.FetchTimeMillis, 0.001},
			{"search_in_flight", "gauge", "fetch", search.FetchCurrent, 1},
		} {
			if metric.value != nil {
				addSample(&samples, "gopulse_elasticsearch_"+metric.name, metric.kind, strconv.FormatFloat(float64(*metric.value)*metric.scale, 'g', -1, 64), map[string]string{"node_id": nodeID, "operation": metric.operation})
			}
		}
		indexing := node.Indices.Indexing
		for _, metric := range []struct {
			name, kind string
			value      *uint64
			scale      float64
		}{
			{"indexing_operations_total", "counter", indexing.IndexTotal, 1},
			{"indexing_duration_seconds_total", "counter", indexing.IndexTimeMillis, 0.001},
			{"indexing_in_flight", "gauge", indexing.IndexCurrent, 1},
			{"indexing_failed_total", "counter", indexing.IndexFailed, 1},
		} {
			if metric.value != nil {
				addSample(&samples, "gopulse_elasticsearch_"+metric.name, metric.kind, strconv.FormatFloat(float64(*metric.value)*metric.scale, 'g', -1, 64), labels)
			}
		}
		for _, cache := range []struct {
			name  string
			stats cacheStats
		}{
			{name: "query", stats: node.Indices.QueryCache},
			{name: "request", stats: node.Indices.RequestCache},
		} {
			cacheLabels := map[string]string{"node_id": nodeID, "cache": cache.name}
			if cache.stats.MemorySize != nil {
				addSample(&samples, "gopulse_elasticsearch_cache_memory_bytes", "gauge", strconv.FormatUint(*cache.stats.MemorySize, 10), cacheLabels)
			}
			if cache.stats.HitCount != nil {
				addSample(&samples, "gopulse_elasticsearch_cache_hits_total", "counter", strconv.FormatUint(*cache.stats.HitCount, 10), cacheLabels)
			}
			if cache.stats.MissCount != nil {
				addSample(&samples, "gopulse_elasticsearch_cache_misses_total", "counter", strconv.FormatUint(*cache.stats.MissCount, 10), cacheLabels)
			}
			if cache.stats.Evictions != nil {
				addSample(&samples, "gopulse_elasticsearch_cache_evictions_total", "counter", strconv.FormatUint(*cache.stats.Evictions, 10), cacheLabels)
			}
		}
		for _, metric := range []struct {
			name  string
			kind  string
			value *uint64
			scale float64
		}{
			{name: "merge_operations_total", kind: "counter", value: node.Indices.Merges.Total, scale: 1},
			{name: "merge_duration_seconds_total", kind: "counter", value: node.Indices.Merges.TotalTime, scale: 0.001},
			{name: "merge_in_flight", kind: "gauge", value: node.Indices.Merges.Current, scale: 1},
			{name: "merge_current_size_bytes", kind: "gauge", value: node.Indices.Merges.CurrentSize, scale: 1},
			{name: "refresh_operations_total", kind: "counter", value: node.Indices.Refresh.Total, scale: 1},
			{name: "refresh_duration_seconds_total", kind: "counter", value: node.Indices.Refresh.TotalTime, scale: 0.001},
			{name: "refresh_listeners", kind: "gauge", value: node.Indices.Refresh.Listeners, scale: 1},
			{name: "flush_operations_total", kind: "counter", value: node.Indices.Flush.Total, scale: 1},
			{name: "flush_duration_seconds_total", kind: "counter", value: node.Indices.Flush.TotalTime, scale: 0.001},
			{name: "fielddata_memory_bytes", kind: "gauge", value: node.Indices.Fielddata.MemorySize, scale: 1},
			{name: "segments_count", kind: "gauge", value: node.Indices.Segments.Count, scale: 1},
			{name: "segments_memory_bytes", kind: "gauge", value: node.Indices.Segments.Memory, scale: 1},
		} {
			if metric.value != nil {
				addSample(&samples, "gopulse_elasticsearch_"+metric.name, metric.kind, strconv.FormatFloat(float64(*metric.value)*metric.scale, 'g', -1, 64), labels)
			}
		}
		for _, breakerName := range supportedCircuitBreakers {
			breaker, ok := node.Breakers[breakerName]
			if !ok {
				continue
			}
			breakerLabels := map[string]string{"node_id": nodeID, "breaker": breakerName}
			if breaker.Tripped != nil {
				addSample(&samples, "gopulse_elasticsearch_circuit_breaker_tripped_total", "counter", strconv.FormatUint(*breaker.Tripped, 10), breakerLabels)
			}
			if breaker.EstimatedSize != nil {
				addSample(&samples, "gopulse_elasticsearch_circuit_breaker_estimated_bytes", "gauge", strconv.FormatUint(*breaker.EstimatedSize, 10), breakerLabels)
			}
			if breaker.LimitSize != nil {
				addSample(&samples, "gopulse_elasticsearch_circuit_breaker_limit_bytes", "gauge", strconv.FormatUint(*breaker.LimitSize, 10), breakerLabels)
			}
		}
	}
	return renderSamples(samples), nil
}

func validLabel(value string) bool {
	return value != "" && utf8.ValidString(value) && len(value) <= 256 && !strings.ContainsRune(value, 0)
}
