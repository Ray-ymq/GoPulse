package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	var brokersValue, topic, group, clientID string
	flag.StringVar(&brokersValue, "brokers", "", "comma-separated Kafka bootstrap brokers")
	flag.StringVar(&topic, "topic", "", "topic to join")
	flag.StringVar(&group, "group", "", "consumer group to join")
	flag.StringVar(&clientID, "client-id", "gopulse-verify-group-member", "verification client identity")
	flag.Parse()

	brokers := splitNonEmpty(brokersValue)
	if len(brokers) == 0 || topic == "" || group == "" || clientID == "" {
		log.Fatal("brokers, topic, group, and client-id are required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID(clientID),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
			for assignedTopic, partitions := range assigned {
				for _, partition := range partitions {
					log.Printf("assigned topic=%s partition=%d", assignedTopic, partition)
				}
			}
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, revoked map[string][]int32) {
			for revokedTopic, partitions := range revoked {
				for _, partition := range partitions {
					log.Printf("revoked topic=%s partition=%d", revokedTopic, partition)
				}
			}
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	log.Printf("joining group=%s topic=%s client_id=%s", group, topic, clientID)
	for ctx.Err() == nil {
		fetches := client.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 && ctx.Err() == nil {
			log.Printf("poll errors=%d", len(errs))
		}
	}
	fmt.Println("stopped")
}

func splitNonEmpty(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}
