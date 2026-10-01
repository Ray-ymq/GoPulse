package componentmetrics

import (
	"fmt"
	"sort"
	"strings"
)

var Components = []string{"backend", "business-worker", "search-indexer", "monitor", "router", "marshaller"}
var Plugins = []string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"}

// ProcessIDs is the code-owned process identity inventory. Runtime roles,
// replica ownership, budgets and Compose bindings remain authoritative in
// deploy/runtime-contracts.json; the verifier rejects drift between this
// closed metric directory and that machine contract.
func ProcessIDs() []string {
	result := append([]string{}, Components...)
	for _, plugin := range Plugins {
		result = append(result, plugin+"-exporter")
	}
	return result
}

func IsComponent(id string) bool {
	for _, c := range Components {
		if c == id {
			return true
		}
	}
	return false
}
func Prefix(id string) string { return "gopulse_" + strings.ReplaceAll(id, "-", "_") + "_" }
func Target(id string) string { return id + "-local" }

type Family struct {
	Name, Kind, Unit string
	Keys             []string
	Tuples           [][]string
	Required         bool
	// Pair identifies the duration counter for a count family (and vice versa).
	Pair string
	// Distribution describes a physical member of a fixed histogram contract.
	// Bucket, count, and sum are separate Prometheus families so every ingress
	// boundary can validate the complete distribution without accepting a
	// client-defined metric name or label.
	Distribution *Distribution
}
type Distribution struct {
	Name    string
	Role    string
	Buckets []string
}
type Spec struct {
	ID                       string
	Families                 []Family
	MaxSamples, MaxBodyBytes int
}

// MessagePairs is the existing strict message contract plus one explicit bucket
// for inputs whose identity cannot be trusted. It does not admit arbitrary IDs.
func MessagePairs() [][]string {
	pairs := [][]string{{"unknown", "unknown"}, {"events", "monitor"}}
	for _, s := range append(append([]string{}, Plugins...), Components...) {
		pairs = append(pairs, []string{"metrics", s})
	}
	for _, s := range []string{"backend", "business-worker", "search-indexer", "search-reindex"} {
		pairs = append(pairs, []string{"logs", s})
	}
	return pairs
}
func MessageIdentity(kind, source string) (string, string) {
	for _, p := range MessagePairs() {
		if p[0] == kind && p[1] == source {
			return kind, source
		}
	}
	return "unknown", "unknown"
}
func ScrapeTargets() [][]string {
	var result [][]string
	for _, id := range Plugins {
		result = append(result, []string{"exporter_plugin", id + "-exporter-local"})
	}
	for _, id := range Components {
		endpoints, err := ReplicaEndpoints(id)
		if err != nil {
			endpoints = []string{id}
		}
		for _, endpoint := range endpoints {
			result = append(result, []string{"component", TargetFor(id, endpoint)})
		}
	}
	return result
}
func expand(base [][]string, values ...string) [][]string {
	var out [][]string
	for _, b := range base {
		for _, v := range values {
			out = append(out, append(append([]string{}, b...), v))
		}
	}
	return out
}
func singles(values ...string) [][]string {
	var out [][]string
	for _, v := range values {
		out = append(out, []string{v})
	}
	return out
}

func partitionTuples() [][]string {
	values := make([][]string, 0, 16)
	for partition := 0; partition < 16; partition++ {
		values = append(values, []string{fmt.Sprintf("%d", partition)})
	}
	return values
}

// Catalog is the authoritative producer, Monitor, Marshaller and Backend
// allowlist. All tuples are constructed from code constants, never config/input.
func Catalog(id string) (Spec, bool) {
	s := Spec{ID: id, MaxBodyBytes: 262144}
	if id == "backend" {
		s.MaxBodyBytes = BackendMaxBodyBytes
	}
	prefix := Prefix(id)
	gauge := func(name, unit string, keys []string, tuples [][]string) {
		if tuples == nil {
			tuples = [][]string{{}}
		}
		s.Families = append(s.Families, Family{Name: prefix + name, Kind: "gauge", Unit: unit, Keys: keys, Tuples: tuples, Required: true})
	}
	pair := func(count, duration string, keys []string, tuples [][]string) {
		s.Families = append(s.Families, Family{Name: prefix + count, Kind: "counter", Unit: "count", Keys: keys, Tuples: tuples, Pair: prefix + duration}, Family{Name: prefix + duration, Kind: "counter", Unit: "seconds", Keys: keys, Tuples: tuples, Pair: prefix + count})
	}
	dep := func(names ...string) { gauge("dependency_up", "state", []string{"dependency"}, singles(names...)) }
	switch id {
	case "backend":
		var routes [][]string
		for _, r := range BackendRoutes() {
			routes = append(routes, []string{r.Method, r.Template})
		}
		for _, method := range backendMethods {
			routes = append(routes, []string{method, "_unmatched"})
		}
		requestTuples := expand(routes, "1xx", "2xx", "3xx", "4xx", "5xx")
		pair("http_requests_total", "http_request_duration_seconds_total", []string{"method", "route", "status_class"}, requestTuples)
		latencyBase := prefix + "http_request_duration_seconds"
		latencyBuckets := BackendLatencyBuckets()
		s.Families = append(s.Families,
			Family{
				Name: prefix + "http_request_duration_seconds_bucket", Kind: "counter", Unit: "count",
				Keys: []string{"method", "route", "status_class", "le"}, Tuples: expand(requestTuples, latencyBuckets...),
				Distribution: &Distribution{Name: latencyBase, Role: "bucket", Buckets: append([]string(nil), latencyBuckets...)},
			},
			Family{
				Name: prefix + "http_request_duration_seconds_count", Kind: "counter", Unit: "count",
				Keys: []string{"method", "route", "status_class"}, Tuples: requestTuples,
				Distribution: &Distribution{Name: latencyBase, Role: "count", Buckets: append([]string(nil), latencyBuckets...)},
			},
			Family{
				Name: latencyBase + "_sum", Kind: "counter", Unit: "seconds",
				Keys: []string{"method", "route", "status_class"}, Tuples: requestTuples,
				Distribution: &Distribution{Name: latencyBase, Role: "sum", Buckets: append([]string(nil), latencyBuckets...)},
			},
		)
		gauge("alert_evaluation_known", "state", []string{"alert_source"}, singles("metrics", "logs", "events"))
		gauge("alert_last_success_timestamp_seconds", "unix_seconds", []string{"alert_source"}, singles("metrics", "logs", "events"))
		gauge("outbox_pending", "count", nil, nil)
		gauge("outbox_oldest_age_seconds", "seconds", nil, nil)
		gauge("outbox_last_publish_success_timestamp_seconds", "unix_seconds", nil, nil)
		gauge("http_requests_in_flight", "count", nil, nil)
		gauge("http_concurrency_limit", "count", nil, nil)
		s.Families = append(s.Families, Family{Name: prefix + "http_rejected_total", Kind: "counter", Unit: "count", Tuples: [][]string{{}}, Required: true})
		pair("freshness_events_total", "freshness_duration_seconds_total", []string{"stage", "result"}, expand(singles("commit", "publish", "consume", "index", "visible"), "success", "failure"))
		s.Families = append(s.Families, Family{Name: prefix + "trace_context_invalid_total", Kind: "counter", Unit: "count", Tuples: [][]string{{}}, Required: true})
		dep("mysql", "redis", "rabbitmq", "elasticsearch")
	case "business-worker":
		pair("messages_total", "message_processing_duration_seconds_total", []string{"event_type", "result"}, expand(singles("comment.created", "post.liked", "user.followed", "unknown"), "success", "retry", "failure", "ack"))
		pair("freshness_events_total", "freshness_duration_seconds_total", []string{"stage", "result"}, expand(singles("consume"), "success", "failure"))
		s.Families = append(s.Families, Family{Name: prefix + "trace_context_invalid_total", Kind: "counter", Unit: "count", Tuples: [][]string{{}}, Required: true})
		gauge("messages_in_flight", "count", nil, nil)
		gauge("prefetch_limit", "count", nil, nil)
		gauge("last_success_timestamp_seconds", "unix_seconds", nil, nil)
		dep("mysql", "rabbitmq")
	case "search-indexer":
		pair("messages_total", "message_processing_duration_seconds_total", []string{"operation", "result"}, expand(singles("create", "update", "delete"), "success", "retry", "failure"))
		pair("freshness_events_total", "freshness_duration_seconds_total", []string{"stage", "result"}, expand(singles("consume", "index", "visible"), "success", "failure"))
		s.Families = append(s.Families, Family{Name: prefix + "trace_context_invalid_total", Kind: "counter", Unit: "count", Tuples: [][]string{{}}, Required: true})
		gauge("messages_in_flight", "count", nil, nil)
		gauge("retrying", "count", nil, nil)
		gauge("last_success_timestamp_seconds", "unix_seconds", nil, nil)
		dep("mysql", "rabbitmq", "elasticsearch")
	case "monitor":
		pair("scrapes_total", "scrape_duration_seconds_total", []string{"scraped_producer_kind", "scraped_target_id", "result"}, expand(ScrapeTargets(), "scrape_success", "scrape_failure", "publish_success", "publish_failure"))
		gauge("last_scrape_success_timestamp_seconds", "unix_seconds", []string{"scraped_producer_kind", "scraped_target_id"}, ScrapeTargets())
		gauge("event_queue_length", "count", nil, nil)
		gauge("plugins_running", "count", nil, nil)
		dep("router")
		s.Families = append(s.Families, Family{Name: prefix + "event_queue_dropped_total", Kind: "counter", Unit: "count", Tuples: [][]string{{}}, Required: true})
	case "router":
		pair("messages_total", "produce_duration_seconds_total", []string{"type", "message_source", "result"}, expand(MessagePairs(), "accepted", "rejected", "produced"))
		gauge("buffered_records", "count", nil, nil)
		gauge("buffered_bytes", "bytes", nil, nil)
		gauge("buffer_limit_records", "count", nil, nil)
		gauge("buffer_limit_bytes", "bytes", nil, nil)
		gauge("last_kafka_ack_timestamp_seconds", "unix_seconds", nil, nil)
		pair("backpressure_total", "backpressure_duration_seconds_total", []string{"reason"}, singles("buffer_full", "kafka_failure", "request_canceled"))
		dep("kafka")
	case "marshaller":
		var tuples [][]string
		for _, p := range MessagePairs() {
			for _, v := range [][]string{{"consume", "consumed"}, {"validate", "validated"}, {"validate", "rejected"}, {"store", "stored"}, {"store", "retried"}, {"commit", "committed"}, {"commit", "failure"}} {
				tuples = append(tuples, append(append([]string{}, p...), v...))
			}
		}
		pair("records_total", "record_processing_duration_seconds_total", []string{"type", "message_source", "stage", "result"}, tuples)
		gauge("records_in_flight", "count", nil, nil)
		gauge("retrying", "count", nil, nil)
		gauge("partition_ownership", "state", []string{"partition"}, partitionTuples())
		gauge("partition_lag", "count", []string{"partition"}, partitionTuples())
		gauge("partition_generation", "count", []string{"partition"}, partitionTuples())
		gauge("target_blocked", "state", []string{"storage"}, singles("victoriametrics", "elasticsearch"))
		gauge("last_storage_success_timestamp_seconds", "unix_seconds", []string{"storage"}, singles("victoriametrics", "elasticsearch"))
		gauge("last_commit_success_timestamp_seconds", "unix_seconds", nil, nil)
		dep("kafka", "victoriametrics", "elasticsearch")
		cleanupResults := []string{"deleted", "not_found", "not_owned", "invalid_date", "not_expired", "transient", "permission_denied", "budget_exhausted", "unknown"}
		streams := singles("logs", "events")
		pair("retention_cleanup_total", "retention_cleanup_duration_seconds_total", []string{"stream", "result"}, expand(streams, cleanupResults...))
		s.Families = append(s.Families,
			Family{Name: prefix + "retention_late_records_total", Kind: "counter", Unit: "count", Keys: []string{"stream", "result"}, Tuples: expand(streams, "accepted", "expired", "invalid_date")},
			Family{Name: prefix + "retention_retries_total", Kind: "counter", Unit: "count", Keys: []string{"stream", "result"}, Tuples: expand(streams, "transient", "permission_denied", "unknown")},
		)
		gauge("retention_last_success_timestamp_seconds", "unix_seconds", []string{"stream"}, streams)
		gauge("retention_blocked", "state", []string{"stream"}, streams)
	default:
		return Spec{}, false
	}
	for _, f := range s.Families {
		s.MaxSamples += len(f.Tuples)
	}
	return s, true
}
func tupleKey(values []string) string { return strings.Join(values, "\x00") }
func labelText(keys, values []string) string {
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%q", k, values[i])
	}
	return "{" + strings.Join(parts, ",") + "}"
}
func Labels(f Family, values []string) map[string]string {
	m := make(map[string]string, len(f.Keys))
	for i, k := range f.Keys {
		m[k] = values[i]
	}
	return m
}
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
