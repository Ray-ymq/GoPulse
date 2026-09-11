package collector

import (
	"context"
	"fmt"
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
		if strings.Count(body, "# TYPE ") != 7 || !strings.Contains(body, "gopulse_kafka_consumer_group_lag 3\n") || !strings.Contains(body, "gopulse_kafka_brokers 1\n") {
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
