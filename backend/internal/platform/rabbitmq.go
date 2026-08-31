package platform

import (
	"context"
	"errors"
	"net"
	"net/url"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQ struct {
	connectionURL string
}

func NewRabbitMQ(connectionURL string) (*RabbitMQ, error) {
	parsed, err := url.Parse(connectionURL)
	if err != nil || (parsed.Scheme != "amqp" && parsed.Scheme != "amqps") || parsed.Host == "" {
		return nil, errors.New("create RabbitMQ checker: invalid AMQP URL")
	}
	return &RabbitMQ{connectionURL: connectionURL}, nil
}

func (checker *RabbitMQ) Check(ctx context.Context) error {
	dialer := &net.Dialer{Timeout: time.Second}
	amqpConfig := amqp.Config{
		Dial: func(network, address string) (net.Conn, error) {
			connection, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			if deadline, ok := ctx.Deadline(); ok {
				if err := connection.SetDeadline(deadline); err != nil {
					_ = connection.Close()
					return nil, err
				}
			}
			return connection, nil
		},
	}

	connection, err := amqp.DialConfig(checker.connectionURL, amqpConfig)
	if err != nil {
		return err
	}
	return connection.Close()
}
