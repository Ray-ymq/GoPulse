package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type evidence struct {
	Key         string `json:"key"`
	ValueBase64 string `json:"value_base64"`
	Partition   int32  `json:"partition"`
	Offset      int64  `json:"offset"`
}

func main() {
	var brokersText, topic, clientID string
	var partition int
	var start, end int64
	var timeout time.Duration
	flag.StringVar(&brokersText, "brokers", "127.0.0.1:9092", "comma-separated Kafka brokers")
	flag.StringVar(&topic, "topic", "gopulse-observability-v1", "Kafka topic")
	flag.StringVar(&clientID, "client-id", "gopulse-verify-consumer", "unique verification client identity")
	flag.IntVar(&partition, "partition", 0, "partition to read")
	flag.Int64Var(&start, "start", 0, "inclusive start offset")
	flag.Int64Var(&end, "end", -1, "exclusive end offset")
	flag.DurationVar(&timeout, "timeout", 10*time.Second, "read timeout")
	flag.Parse()
	if topic != "gopulse-observability-v1" || !strings.HasPrefix(clientID, "gopulse-verify-") || len(clientID) > 128 || partition < 0 || start < 0 || end <= start || timeout <= 0 {
		log.Fatal("invalid bounded consumer arguments")
	}
	brokers := strings.Split(brokersText, ",")
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID(clientID),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			topic: {int32(partition): kgo.NewOffset().At(start)},
		}),
	)
	if err != nil {
		log.Fatalf("consumer initialization failed: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	encoder := json.NewEncoder(os.Stdout)
	next := start
	for next < end {
		fetches := client.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			log.Fatal("consumer fetch failed")
		}
		fetches.EachRecord(func(record *kgo.Record) {
			if record.Partition != int32(partition) || record.Offset < start || record.Offset >= end {
				return
			}
			if err := encoder.Encode(evidence{
				Key: string(record.Key), ValueBase64: base64.StdEncoding.EncodeToString(record.Value),
				Partition: record.Partition, Offset: record.Offset,
			}); err != nil {
				log.Fatal("consumer output failed")
			}
			if record.Offset >= next {
				next = record.Offset + 1
			}
		})
		if ctx.Err() != nil {
			log.Fatal("consumer timed out")
		}
	}
	fmt.Fprintf(os.Stderr, "read offsets [%d,%d) from partition %d\n", start, end, partition)
}
