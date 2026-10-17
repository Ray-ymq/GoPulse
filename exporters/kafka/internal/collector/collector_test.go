package collector

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kmsg"
)

type readOnlyFixture struct {
	t       *testing.T
	missing bool
	cold    bool
}

func (*readOnlyFixture) Close() {}
func (f *readOnlyFixture) Request(_ context.Context, req kmsg.Request) (kmsg.Response, error) {
	switch r := req.(type) {
	case *kmsg.MetadataRequest:
		if r.AllowAutoTopicCreation || len(r.Topics) != 2 || r.Topics[0].Topic == nil || *r.Topics[0].Topic != Topic || r.Topics[1].Topic == nil || *r.Topics[1].Topic != "__consumer_offsets" {
			f.t.Fatal("unsafe metadata request")
		}
		topic := Topic
		offsets := "__consumer_offsets"
		internal := kmsg.MetadataResponseTopic{Topic: &offsets, Partitions: []kmsg.MetadataResponseTopicPartition{{Partition: 0}}}
		if f.cold {
			internal.ErrorCode = 3
		}
		return &kmsg.MetadataResponse{ControllerID: 1, Brokers: []kmsg.MetadataResponseBroker{{NodeID: 1}}, Topics: []kmsg.MetadataResponseTopic{{Topic: &topic, Partitions: []kmsg.MetadataResponseTopicPartition{{Partition: 0, Leader: 1, Replicas: []int32{1}, ISR: []int32{1}}}}, internal}}, nil
	case *kmsg.ListOffsetsRequest:
		if len(r.Topics) != 1 || r.Topics[0].Topic != Topic || len(r.Topics[0].Partitions) != 1 || r.Topics[0].Partitions[0].Timestamp != -1 {
			f.t.Fatal("unexpected log-end request")
		}
		return &kmsg.ListOffsetsResponse{Topics: []kmsg.ListOffsetsResponseTopic{{Topic: Topic, Partitions: []kmsg.ListOffsetsResponseTopicPartition{{Partition: 0, Offset: 9}}}}}, nil
	case *kmsg.OffsetFetchRequest:
		if f.cold {
			f.t.Fatal("coordinator request may initialize internal topic")
		}
		if r.Group != Group || len(r.Topics) != 1 || r.Topics[0].Topic != Topic || len(r.Topics[0].Partitions) != 1 {
			f.t.Fatal("unexpected group request")
		}
		offset := int64(6)
		if f.missing {
			offset = -1
		}
		return &kmsg.OffsetFetchResponse{Topics: []kmsg.OffsetFetchResponseTopic{{Topic: Topic, Partitions: []kmsg.OffsetFetchResponseTopicPartition{{Partition: 0, Offset: offset}}}}}, nil
	default:
		f.t.Errorf("unexpected request (writes forbidden): %T", req)
		return nil, fmt.Errorf("forbidden")
	}
}

// Proves the changed missing-offset contract at the lowest layer. This is not
// evidence of a real partial Kafka topology or formal Marshaller consumption.
func TestMissingOffsetAndSnapshotRecovery(t *testing.T) {
	f := &readOnlyFixture{t: t}
	c := &Client{Kafka: f}
	for _, missing := range []bool{true, false} {
		f.missing = missing
		body, err := Collect(context.Background(), c)
		if missing {
			if err != ErrUnavailable || body != "" {
				t.Fatalf("missing offset filled: %q %v", body, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "gopulse_kafka_consumer_group_lag 3\n") || !strings.Contains(body, "gopulse_kafka_brokers 1\n") || !strings.Contains(body, `gopulse_kafka_consumer_partition_lag{consumer_group="gopulse-marshaller-metrics",topic="gopulse-observability-metrics",partition="0"} 3`) || !strings.Contains(body, `gopulse_kafka_partition_replicas{consumer_group="gopulse-marshaller-metrics",topic="gopulse-observability-metrics",partition="0"} 1`) {
			t.Fatal(body)
		}
	}
}

func TestColdMetadataDoesNotInitializeOffsetsTopic(t *testing.T) {
	f := &readOnlyFixture{t: t, cold: true}
	if body, err := Collect(context.Background(), &Client{Kafka: f}); err != ErrUnavailable || body != "" {
		t.Fatal("cold metadata must fail before coordinator request", body, err)
	}
}

func TestOptionalJMXSnapshotAndFailureSignal(t *testing.T) {
	f := &readOnlyFixture{t: t}
	failed := false
	invalidLabels := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/metrics" {
			t.Errorf("unexpected JMX request: %s %s", r.Method, r.URL.Path)
		}
		if failed {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if invalidLabels {
			w.Write([]byte("# TYPE kafka_server_brokertopicmetrics_bytesin_total counter\nkafka_server_brokertopicmetrics_bytesin_total{topic=\"orders\",client=\"unexpected\"} 100\n"))
			return
		}
		w.Write([]byte(strings.TrimSpace(`
# TYPE kafka_server_brokertopicmetrics_bytesin_total counter
kafka_server_brokertopicmetrics_bytesin_total{topic="orders"} 100
# TYPE kafka_network_requestmetrics_requests_total counter
kafka_network_requestmetrics_requests_total{request="Produce"} 4
# TYPE kafka_network_requestmetrics_errors_total counter
kafka_network_requestmetrics_errors_total{request="Produce",error="NONE",version="12"} 1
# TYPE kafka_network_requestmetrics_totaltimems_total counter
kafka_network_requestmetrics_totaltimems_total{request="Produce"} 2500
# TYPE kafka_network_processor_avgidle_percent gauge
kafka_network_processor_avgidle_percent{processor="0"} 75
# TYPE java_lang_memory_heapmemoryusage_used gauge
java_lang_memory_heapmemoryusage_used 200
# TYPE java_lang_garbagecollector_collectiontime gauge
java_lang_garbagecollector_collectiontime{name="G1 Young"} 12
# TYPE java_lang_garbagecollector_collectioncount counter
java_lang_garbagecollector_collectioncount{name="G1 Young"} 3
# TYPE java_lang_garbagecollector_collectioncount_total counter
java_lang_garbagecollector_collectioncount_total{gc="G1 Old Generation"} 4
# TYPE java_lang_garbagecollector_collectiontime_total counter
java_lang_garbagecollector_collectiontime_total{gc="G1 Old Generation"} 8
# TYPE gopulse_fake_metric gauge
gopulse_fake_metric 999
`) + "\n"))
	}))
	defer server.Close()
	c := &Client{Kafka: f, JMXURL: server.URL + "/metrics", JMXHTTP: server.Client()}
	body, err := Collect(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"gopulse_kafka_jmx_up 1",
		`gopulse_kafka_broker_bytes_in_total{topic="orders"} 100`,
		`gopulse_kafka_request_duration_seconds_total{request="Produce"} 2.5`,
		`gopulse_kafka_request_errors_total{error="NONE",request="Produce",version="12"} 1`,
		`gopulse_kafka_network_processor_idle_ratio{processor="0"} 0.75`,
		"gopulse_kafka_jvm_heap_used_bytes 200",
		`gopulse_kafka_jvm_gc_collection_time_seconds_total{gc="G1 Young"} 0.012`,
		`gopulse_kafka_jvm_gc_collections_total{gc="G1 Young"} 3`,
		`gopulse_kafka_jvm_gc_collection_time_seconds_total{gc="G1 Old Generation"} 0.008`,
		`gopulse_kafka_jvm_gc_collections_total{gc="G1 Old Generation"} 4`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "gopulse_fake_metric") {
		t.Fatal("unknown JMX metric leaked")
	}
	failed = true
	body, err = Collect(context.Background(), c)
	if err != nil || !strings.Contains(body, "gopulse_kafka_jmx_up 0\n") || !strings.Contains(body, "gopulse_kafka_up 1\n") {
		t.Fatalf("JMX failure damaged Kafka snapshot: %q %v", body, err)
	}
	invalidLabels = true
	failed = false
	body, err = Collect(context.Background(), c)
	if err != nil || !strings.Contains(body, "gopulse_kafka_jmx_up 0\n") || strings.Contains(body, "broker_bytes_in_total") {
		t.Fatalf("invalid JMX labels accepted: %q %v", body, err)
	}
}
