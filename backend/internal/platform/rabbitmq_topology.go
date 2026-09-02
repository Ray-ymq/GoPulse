package platform

import (
	"errors"
	"fmt"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	BusinessExchange      = "gopulse.business.v1"
	BusinessQueue         = "gopulse.business-worker.v1"
	BusinessRetryExchange = "gopulse.business.retry.v1"
	BusinessRetryQueue    = "gopulse.business-worker.retry.v1"
	BusinessDeadExchange  = "gopulse.business.dead.v1"
	BusinessDeadQueue     = "gopulse.business-worker.dead.v1"

	minimumBusinessRetryDelay = time.Second
	maximumBusinessRetryDelay = 24 * time.Hour
)

var businessRoutingKeys = []string{bus.CommentCreatedRoutingKey, bus.PostLikedRoutingKey}

type AMQPTopologyDeclarer interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, arguments amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, arguments amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, arguments amqp.Table) error
}

func DeclareBusinessTopology(channel AMQPTopologyDeclarer, retryDelay time.Duration) error {
	if channel == nil {
		return errors.New("declare business topology: channel is required")
	}
	if retryDelay < minimumBusinessRetryDelay || retryDelay > maximumBusinessRetryDelay {
		return fmt.Errorf("declare business topology: retry delay must be between %s and %s", minimumBusinessRetryDelay, maximumBusinessRetryDelay)
	}

	for _, exchange := range []string{BusinessExchange, BusinessRetryExchange, BusinessDeadExchange} {
		if err := channel.ExchangeDeclare(exchange, amqp.ExchangeDirect, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare business topology exchange %s: %w", exchange, err)
		}
	}

	if _, err := channel.QueueDeclare(BusinessQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare business topology queue %s: %w", BusinessQueue, err)
	}
	retryArguments := amqp.Table{
		"x-message-ttl":          retryDelay.Milliseconds(),
		"x-dead-letter-exchange": BusinessExchange,
	}
	if _, err := channel.QueueDeclare(BusinessRetryQueue, true, false, false, false, retryArguments); err != nil {
		return fmt.Errorf("declare business topology queue %s: %w", BusinessRetryQueue, err)
	}
	if _, err := channel.QueueDeclare(BusinessDeadQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare business topology queue %s: %w", BusinessDeadQueue, err)
	}

	for _, routingKey := range businessRoutingKeys {
		if err := channel.QueueBind(BusinessQueue, routingKey, BusinessExchange, false, nil); err != nil {
			return fmt.Errorf("bind business topology queue %s: %w", BusinessQueue, err)
		}
		if err := channel.QueueBind(BusinessRetryQueue, routingKey, BusinessRetryExchange, false, nil); err != nil {
			return fmt.Errorf("bind business topology queue %s: %w", BusinessRetryQueue, err)
		}
		if err := channel.QueueBind(BusinessDeadQueue, routingKey, BusinessDeadExchange, false, nil); err != nil {
			return fmt.Errorf("bind business topology queue %s: %w", BusinessDeadQueue, err)
		}
	}
	return nil
}
