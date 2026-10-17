// Package collector issues only metadata, list-offset and offset-fetch requests.
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
	"unicode/utf8"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const Topic = "gopulse-observability-metrics"
const Group = "gopulse-marshaller-metrics"
const Unavailable = "# TYPE gopulse_kafka_up gauge\ngopulse_kafka_up 0\n"

var ErrUnavailable = errors.New("target_unavailable")

type readClient interface {
	kmsg.Requestor
	Close()
}
type Client struct {
	Kafka   readClient
	JMXHTTP *http.Client
	JMXURL  string
}

func (c *Client) CloseIdleConnections() {
	if c.Kafka != nil {
		c.Kafka.Close()
	}
	if c.JMXHTTP != nil {
		c.JMXHTTP.CloseIdleConnections()
	}
}

func Collect(ctx context.Context, c *Client) (string, error) {
	req := kmsg.NewPtrMetadataRequest()
	req.AllowAutoTopicCreation = false
	topic := Topic
	offsetsTopic := "__consumer_offsets"
	req.Topics = []kmsg.MetadataRequestTopic{{Topic: &topic}, {Topic: &offsetsTopic}}
	meta, err := req.RequestWith(ctx, c.Kafka)
	if err != nil || len(meta.Brokers) == 0 || len(meta.Topics) != 2 {
		return "", ErrUnavailable
	}
	// FindCoordinator can make the broker create __consumer_offsets on a
	// pristine cluster. Read metadata with auto-creation disabled first; only
	// formal consumption, never an Exporter check, may initialize that topic.
	var t kmsg.MetadataResponseTopic
	fixed, offsetsPresent := false, false
	for _, item := range meta.Topics {
		if item.Topic == nil || item.ErrorCode != 0 {
			return "", ErrUnavailable
		}
		switch *item.Topic {
		case Topic:
			if fixed {
				return "", ErrUnavailable
			}
			t = item
			fixed = true
		case offsetsTopic:
			if offsetsPresent {
				return "", ErrUnavailable
			}
			offsetsPresent = len(item.Partitions) > 0
		default:
			return "", ErrUnavailable
		}
	}
	if !fixed || !offsetsPresent {
		return "", ErrUnavailable
	}
	if t.ErrorCode != 0 || t.Topic == nil || *t.Topic != Topic || len(t.Partitions) == 0 || len(t.Partitions) > 1024 {
		return "", ErrUnavailable
	}
	brokerIDs := map[int32]bool{}
	for _, b := range meta.Brokers {
		if brokerIDs[b.NodeID] {
			return "", ErrUnavailable
		}
		brokerIDs[b.NodeID] = true
	}
	controller := 0
	if brokerIDs[meta.ControllerID] {
		controller = 1
	}
	parts := map[int32]bool{}
	partitionMeta := map[int32]kmsg.MetadataResponseTopicPartition{}
	under, offline := 0, 0
	list := kmsg.NewPtrListOffsetsRequest()
	lt := kmsg.NewListOffsetsRequestTopic()
	lt.Topic = Topic
	fetch := kmsg.NewPtrOffsetFetchRequest()
	fetch.Group = Group
	ft := kmsg.NewOffsetFetchRequestTopic()
	ft.Topic = Topic
	for _, p := range t.Partitions {
		if p.ErrorCode != 0 || p.Partition < 0 || parts[p.Partition] || len(p.Replicas) == 0 {
			return "", ErrUnavailable
		}
		parts[p.Partition] = true
		partitionMeta[p.Partition] = p
		if len(p.ISR) < len(p.Replicas) {
			under++
		}
		if p.Leader < 0 {
			offline++
		}
		lp := kmsg.NewListOffsetsRequestTopicPartition()
		lp.Partition = p.Partition
		lp.Timestamp = -1
		lp.CurrentLeaderEpoch = p.LeaderEpoch
		lt.Partitions = append(lt.Partitions, lp)
		ft.Partitions = append(ft.Partitions, p.Partition)
	}
	list.Topics = []kmsg.ListOffsetsRequestTopic{lt}
	fetch.Topics = []kmsg.OffsetFetchRequestTopic{ft}
	ends, err := list.RequestWith(ctx, c.Kafka)
	if err != nil || len(ends.Topics) != 1 || ends.Topics[0].Topic != Topic || len(ends.Topics[0].Partitions) != len(parts) {
		return "", ErrUnavailable
	}
	end := map[int32]int64{}
	for _, p := range ends.Topics[0].Partitions {
		if _, ok := end[p.Partition]; ok {
			return "", ErrUnavailable
		}
		if !parts[p.Partition] || p.ErrorCode != 0 || p.Offset < 0 {
			return "", ErrUnavailable
		}
		end[p.Partition] = p.Offset
	}
	offsets, err := fetch.RequestWith(ctx, c.Kafka)
	if err != nil || offsets.ErrorCode != 0 || len(offsets.Topics) != 1 || offsets.Topics[0].Topic != Topic || len(offsets.Topics[0].Partitions) != len(parts) {
		return "", ErrUnavailable
	}
	seen := map[int32]bool{}
	committed := map[int32]int64{}
	var lag int64
	for _, p := range offsets.Topics[0].Partitions {
		if !parts[p.Partition] || seen[p.Partition] || p.ErrorCode != 0 || p.Offset < 0 {
			return "", ErrUnavailable
		}
		seen[p.Partition] = true
		committed[p.Partition] = p.Offset
		if d := end[p.Partition] - p.Offset; d > 0 {
			if lag > math.MaxInt64-d {
				return "", ErrUnavailable
			}
			lag += d
		}
	}
	names := []string{"up", "brokers", "controller_available", "partitions", "under_replicated_partitions", "offline_partitions", "consumer_group_lag"}
	values := []int64{1, int64(len(meta.Brokers)), int64(controller), int64(len(parts)), int64(under), int64(offline), lag}
	var b strings.Builder
	for i, n := range names {
		fmt.Fprintf(&b, "# TYPE gopulse_kafka_%s gauge\ngopulse_kafka_%s %d\n", n, n, values[i])
	}
	for _, name := range []string{"partition_log_end_offset", "consumer_committed_offset", "consumer_partition_lag", "partition_replicas", "partition_isr", "partition_under_replicated", "partition_offline", "partition_leader"} {
		fmt.Fprintf(&b, "# TYPE gopulse_kafka_%s gauge\n", name)
	}
	partitions := make([]int, 0, len(parts))
	for partition := range parts {
		partitions = append(partitions, int(partition))
	}
	sort.Ints(partitions)
	for _, value := range partitions {
		partition := int32(value)
		p := partitionMeta[partition]
		labels := fmt.Sprintf(`{consumer_group=%q,topic=%q,partition=%q}`, Group, Topic, strconv.Itoa(value))
		fmt.Fprintf(&b, "gopulse_kafka_partition_log_end_offset%s %d\n", labels, end[partition])
		fmt.Fprintf(&b, "gopulse_kafka_consumer_committed_offset%s %d\n", labels, committed[partition])
		partitionLag := end[partition] - committed[partition]
		if partitionLag < 0 {
			return "", ErrUnavailable
		}
		fmt.Fprintf(&b, "gopulse_kafka_consumer_partition_lag%s %d\n", labels, partitionLag)
		fmt.Fprintf(&b, "gopulse_kafka_partition_replicas%s %d\n", labels, len(p.Replicas))
		fmt.Fprintf(&b, "gopulse_kafka_partition_isr%s %d\n", labels, len(p.ISR))
		underReplicated := 0
		if len(p.ISR) < len(p.Replicas) {
			underReplicated = 1
		}
		offlinePartition := 0
		if p.Leader < 0 {
			offlinePartition = 1
		}
		fmt.Fprintf(&b, "gopulse_kafka_partition_under_replicated%s %d\n", labels, underReplicated)
		fmt.Fprintf(&b, "gopulse_kafka_partition_offline%s %d\n", labels, offlinePartition)
		leaderLabels := fmt.Sprintf(`{topic=%q,partition=%q,node_id=%q}`, Topic, strconv.Itoa(value), strconv.FormatInt(int64(p.Leader), 10))
		leaderAvailable := 0
		if p.Leader >= 0 {
			leaderAvailable = 1
		}
		fmt.Fprintf(&b, "gopulse_kafka_partition_leader%s %d\n", leaderLabels, leaderAvailable)
	}
	body := b.String()
	if c.JMXURL == "" {
		return body, nil
	}
	jmx, err := collectJMX(ctx, c)
	if err != nil {
		return body + "# TYPE gopulse_kafka_jmx_up gauge\ngopulse_kafka_jmx_up 0\n", nil
	}
	return body + renderJMX(jmx), nil
}

type jmxMapping struct {
	upstream     []string
	name         string
	kind         string
	scale        float64
	labelAliases map[string]string
}

type jmxSample struct {
	name, kind string
	labels     map[string]string
	value      string
}

var jmxMappings = []jmxMapping{
	{name: "broker_bytes_in_total", kind: "counter", upstream: []string{"kafka_server_brokertopicmetrics_bytesin_total"}, scale: 1, labelAliases: map[string]string{"topic": "topic"}},
	{name: "broker_bytes_out_total", kind: "counter", upstream: []string{"kafka_server_brokertopicmetrics_bytesout_total"}, scale: 1, labelAliases: map[string]string{"topic": "topic"}},
	{name: "broker_messages_in_total", kind: "counter", upstream: []string{"kafka_server_brokertopicmetrics_messagesin_total"}, scale: 1, labelAliases: map[string]string{"topic": "topic"}},
	{name: "request_total", kind: "counter", upstream: []string{"kafka_network_requestmetrics_requests_total"}, scale: 1, labelAliases: map[string]string{"request": "request", "name": "request", "version": "version"}},
	{name: "request_errors_total", kind: "counter", upstream: []string{"kafka_network_requestmetrics_errors_total"}, scale: 1, labelAliases: map[string]string{"request": "request", "name": "request", "version": "version", "error": "error"}},
	{name: "request_duration_seconds_total", kind: "counter", upstream: []string{"kafka_network_requestmetrics_totaltimems_total"}, scale: 0.001, labelAliases: map[string]string{"request": "request", "name": "request", "version": "version"}},
	{name: "request_queue_duration_seconds_total", kind: "counter", upstream: []string{"kafka_network_requestmetrics_requestqueuetimems_total"}, scale: 0.001, labelAliases: map[string]string{"request": "request", "name": "request", "version": "version"}},
	{name: "request_local_duration_seconds_total", kind: "counter", upstream: []string{"kafka_network_requestmetrics_localtimems_total"}, scale: 0.001, labelAliases: map[string]string{"request": "request", "name": "request", "version": "version"}},
	{name: "request_remote_duration_seconds_total", kind: "counter", upstream: []string{"kafka_network_requestmetrics_remotetimems_total"}, scale: 0.001, labelAliases: map[string]string{"request": "request", "name": "request", "version": "version"}},
	{name: "request_queue_size", kind: "gauge", upstream: []string{"kafka_network_requestchannel_requestqueuesize"}, scale: 1, labelAliases: map[string]string{}},
	{name: "network_processor_idle_ratio", kind: "gauge", upstream: []string{"kafka_network_processor_avgidle_percent", "kafka_network_processor_idle_percent"}, scale: 0.01, labelAliases: map[string]string{"networkProcessor": "processor", "processor": "processor"}},
	{name: "request_handler_idle_ratio", kind: "gauge", upstream: []string{"kafka_server_kafkarequesthandlerpool_requesthandleravgidle_percent"}, scale: 0.01, labelAliases: map[string]string{}},
	{name: "broker_under_replicated_partitions", kind: "gauge", upstream: []string{"kafka_server_replicamanager_underreplicatedpartitions"}, scale: 1, labelAliases: map[string]string{}},
	{name: "isr_shrinks_total", kind: "counter", upstream: []string{"kafka_server_replicamanager_isrshrinks_total"}, scale: 1, labelAliases: map[string]string{}},
	{name: "isr_expands_total", kind: "counter", upstream: []string{"kafka_server_replicamanager_isrexpands_total"}, scale: 1, labelAliases: map[string]string{}},
	{name: "leader_elections_total", kind: "counter", upstream: []string{"kafka_controller_controllerstats_leaderelections_total"}, scale: 1, labelAliases: map[string]string{}},
	{name: "unclean_leader_elections_total", kind: "counter", upstream: []string{"kafka_controller_controllerstats_uncleanleaderelections_total"}, scale: 1, labelAliases: map[string]string{}},
	{name: "jvm_heap_used_bytes", kind: "gauge", upstream: []string{"java_lang_memory_heapmemoryusage_used", "java_lang_memory_heapmemoryusage_used_bytes", "jvm_memory_heap_used_bytes"}, scale: 1, labelAliases: map[string]string{}},
	{name: "jvm_heap_max_bytes", kind: "gauge", upstream: []string{"java_lang_memory_heapmemoryusage_max", "java_lang_memory_heapmemoryusage_max_bytes", "jvm_memory_heap_max_bytes"}, scale: 1, labelAliases: map[string]string{}},
	{name: "jvm_threads", kind: "gauge", upstream: []string{"java_lang_threading_threadcount", "jvm_threads"}, scale: 1, labelAliases: map[string]string{}},
	{name: "jvm_gc_collections_total", kind: "counter", upstream: []string{"java_lang_garbagecollector_collectioncount", "java_lang_garbagecollector_collectioncount_total", "jvm_gc_collections_total"}, scale: 1, labelAliases: map[string]string{"name": "gc", "gc": "gc"}},
	{name: "jvm_gc_collection_time_seconds_total", kind: "counter", upstream: []string{"java_lang_garbagecollector_collectiontime", "java_lang_garbagecollector_collectiontime_total"}, scale: 0.001, labelAliases: map[string]string{"name": "gc", "gc": "gc"}},
	{name: "jvm_gc_collection_time_seconds_total", kind: "counter", upstream: []string{"jvm_gc_collection_time_seconds_total"}, scale: 1, labelAliases: map[string]string{"name": "gc", "gc": "gc"}},
}

func collectJMX(ctx context.Context, c *Client) ([]jmxSample, error) {
	client := c.JMXHTTP
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.JMXURL, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Encoding") != "" {
		return nil, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return nil, ErrUnavailable
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("jmx parse: %w", err)
	}
	if len(families) > 1024 {
		return nil, ErrUnavailable
	}
	lookup := map[string]jmxMapping{}
	for _, mapping := range jmxMappings {
		for _, upstream := range mapping.upstream {
			lookup[upstream] = mapping
		}
	}
	seen := map[string]bool{}
	samples := make([]jmxSample, 0, 64)
	for upstream, family := range families {
		mapping, ok := lookup[upstream]
		if !ok || family == nil {
			continue
		}
		for _, metric := range family.Metric {
			if len(samples) >= 2048 {
				return nil, ErrUnavailable
			}
			labels, err := jmxLabels(metric, mapping.labelAliases)
			if err != nil {
				return nil, fmt.Errorf("jmx labels for %s: %w", upstream, err)
			}
			value, ok := jmxValue(metric)
			if !ok || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("jmx value for %s: %w", upstream, ErrUnavailable)
			}
			value *= mapping.scale
			if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("jmx scaled value for %s: %w", upstream, ErrUnavailable)
			}
			key := mapping.name + formatJMXLabels(labels)
			if seen[key] {
				return nil, fmt.Errorf("jmx duplicate sample for %s: %w", upstream, ErrUnavailable)
			}
			seen[key] = true
			samples = append(samples, jmxSample{name: "gopulse_kafka_" + mapping.name, kind: mapping.kind, labels: labels, value: strconv.FormatFloat(value, 'g', -1, 64)})
		}
	}
	return samples, nil
}

func jmxLabels(metric *dto.Metric, aliases map[string]string) (map[string]string, error) {
	labels := map[string]string{}
	for _, pair := range metric.Label {
		if pair == nil || pair.Name == nil || pair.Value == nil {
			return nil, ErrUnavailable
		}
		name, ok := aliases[pair.GetName()]
		if !ok || !validJMXLabel(pair.GetValue()) || labels[name] != "" {
			return nil, ErrUnavailable
		}
		labels[name] = pair.GetValue()
	}
	return labels, nil
}

func jmxValue(metric *dto.Metric) (float64, bool) {
	switch {
	case metric == nil:
		return 0, false
	case metric.Gauge != nil:
		return metric.Gauge.GetValue(), true
	case metric.Counter != nil:
		return metric.Counter.GetValue(), true
	case metric.Untyped != nil:
		return metric.Untyped.GetValue(), true
	case metric.Summary != nil:
		return metric.Summary.GetSampleSum(), true
	default:
		return 0, false
	}
}

func renderJMX(samples []jmxSample) string {
	types := map[string]string{"gopulse_kafka_jmx_up": "gauge"}
	for _, sample := range samples {
		types[sample.name] = sample.kind
	}
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "# TYPE %s %s\n", name, types[name])
	}
	b.WriteString("gopulse_kafka_jmx_up 1\n")
	sort.Slice(samples, func(i, j int) bool {
		if samples[i].name != samples[j].name {
			return samples[i].name < samples[j].name
		}
		return formatJMXLabels(samples[i].labels) < formatJMXLabels(samples[j].labels)
	})
	for _, sample := range samples {
		fmt.Fprintf(&b, "%s%s %s\n", sample.name, formatJMXLabels(sample.labels), sample.value)
	}
	return b.String()
}

func formatJMXLabels(labels map[string]string) string {
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

func validJMXLabel(value string) bool {
	return value != "" && utf8.ValidString(value) && len(value) <= 128 && !strings.ContainsRune(value, 0)
}
