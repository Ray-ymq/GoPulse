package platform

import (
	"errors"
	"fmt"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	BusinessExchange          = "gopulse.business.v1"
	BusinessQueue             = "gopulse.business-worker.v1"
	BusinessRetryExchange     = "gopulse.business.retry.v1"
	BusinessRetryQueue        = "gopulse.business-worker.retry.v1"
	BusinessDeadExchange      = "gopulse.business.dead.v1"
	BusinessDeadQueue         = "gopulse.business-worker.dead.v1"
	BusinessInvalidRoutingKey = "invalid.v1"

	SearchExchange          = "gopulse.search.v1"
	SearchQueue             = "gopulse.search-indexer.v1"
	SearchRetryExchange     = "gopulse.search.retry.v1"
	SearchRetryQueue        = "gopulse.search-indexer.retry.v1"
	SearchDeadExchange      = "gopulse.search.dead.v1"
	SearchDeadQueue         = "gopulse.search-indexer.dead.v1"
	SearchInvalidRoutingKey = "search.invalid.v1"

	minimumBusinessRetryDelay = time.Second
	maximumBusinessRetryDelay = 24 * time.Hour
)

type Topology struct {
	Exchange          string
	Queue             string
	RetryExchange     string
	RetryQueue        string
	DeadExchange      string
	DeadQueue         string
	InvalidRoutingKey string
	RoutingKeys       []string
}

var (
	BusinessTopology = Topology{
		Exchange: BusinessExchange, Queue: BusinessQueue,
		RetryExchange: BusinessRetryExchange, RetryQueue: BusinessRetryQueue,
		DeadExchange: BusinessDeadExchange, DeadQueue: BusinessDeadQueue,
		InvalidRoutingKey: BusinessInvalidRoutingKey,
		RoutingKeys:       []string{bus.CommentCreatedRoutingKey, bus.PostLikedRoutingKey},
	}
	SearchTopology = Topology{
		Exchange: SearchExchange, Queue: SearchQueue,
		RetryExchange: SearchRetryExchange, RetryQueue: SearchRetryQueue,
		DeadExchange: SearchDeadExchange, DeadQueue: SearchDeadQueue,
		InvalidRoutingKey: SearchInvalidRoutingKey,
		RoutingKeys:       []string{bus.PostCreatedRoutingKey},
	}
)

type AMQPTopologyDeclarer interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, arguments amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, arguments amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, arguments amqp.Table) error
}

func DeclareBusinessTopology(channel AMQPTopologyDeclarer, retryDelay time.Duration) error {
	return DeclareTopology(channel, BusinessTopology, retryDelay)
}

func DeclareSearchTopology(channel AMQPTopologyDeclarer, retryDelay time.Duration) error {
	return DeclareTopology(channel, SearchTopology, retryDelay)
}

func DeclareTopology(channel AMQPTopologyDeclarer, topology Topology, retryDelay time.Duration) error {
	if channel == nil {
		return errors.New("declare RabbitMQ topology: channel is required")
	}
	if err := validateTopology(topology); err != nil {
		return err
	}
	if retryDelay < minimumBusinessRetryDelay || retryDelay > maximumBusinessRetryDelay {
		return fmt.Errorf("declare RabbitMQ topology: retry delay must be between %s and %s", minimumBusinessRetryDelay, maximumBusinessRetryDelay)
	}
	for _, exchange := range []string{topology.Exchange, topology.RetryExchange, topology.DeadExchange} {
		if err := channel.ExchangeDeclare(exchange, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare RabbitMQ topology exchange %s: %w", exchange, err)
		}
	}
	if _, err := channel.QueueDeclare(topology.Queue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare RabbitMQ topology queue %s: %w", topology.Queue, err)
	}
	retryArguments := amqp.Table{"x-message-ttl": retryDelay.Milliseconds(), "x-dead-letter-exchange": topology.Exchange}
	if _, err := channel.QueueDeclare(topology.RetryQueue, true, false, false, false, retryArguments); err != nil {
		return fmt.Errorf("declare RabbitMQ topology queue %s: %w", topology.RetryQueue, err)
	}
	if _, err := channel.QueueDeclare(topology.DeadQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare RabbitMQ topology queue %s: %w", topology.DeadQueue, err)
	}
	for _, routingKey := range topology.RoutingKeys {
		for _, binding := range []struct{ queue, exchange string }{
			{topology.Queue, topology.Exchange}, {topology.RetryQueue, topology.RetryExchange}, {topology.DeadQueue, topology.DeadExchange},
		} {
			if err := channel.QueueBind(binding.queue, routingKey, binding.exchange, false, nil); err != nil {
				return fmt.Errorf("bind RabbitMQ topology queue %s: %w", binding.queue, err)
			}
		}
	}
	if err := channel.QueueBind(topology.DeadQueue, topology.InvalidRoutingKey, topology.DeadExchange, false, nil); err != nil {
		return fmt.Errorf("bind RabbitMQ topology queue %s invalid routing key: %w", topology.DeadQueue, err)
	}
	return nil
}

func validateTopology(topology Topology) error {
	if topology.Exchange == "" || topology.Queue == "" || topology.RetryExchange == "" || topology.RetryQueue == "" || topology.DeadExchange == "" || topology.DeadQueue == "" || topology.InvalidRoutingKey == "" || len(topology.RoutingKeys) == 0 {
		return errors.New("declare RabbitMQ topology: profile is invalid")
	}
	return nil
}
