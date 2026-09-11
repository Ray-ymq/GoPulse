// Package collector issues only metadata, list-offset and offset-fetch requests.
package collector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/twmb/franz-go/pkg/kmsg"
)

const Topic = "gopulse-observability-v1"
const Group = "gopulse-marshaller-metrics-v1"
const Unavailable = "# TYPE gopulse_kafka_up gauge\ngopulse_kafka_up 0\n"

var ErrUnavailable = errors.New("target_unavailable")

type readClient interface {
	kmsg.Requestor
	Close()
}
type Client struct{ Kafka readClient }

func (c *Client) CloseIdleConnections() { c.Kafka.Close() }

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
	var lag int64
	for _, p := range offsets.Topics[0].Partitions {
		if !parts[p.Partition] || seen[p.Partition] || p.ErrorCode != 0 || p.Offset < 0 {
			return "", ErrUnavailable
		}
		seen[p.Partition] = true
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
	return b.String(), nil
}
