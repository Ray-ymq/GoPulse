package worker

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	amqp "github.com/rabbitmq/amqp091-go"
)

type RuntimeOptions struct {
	Prefetch         int
	MaxRetries       int
	RetryDelay       time.Duration
	PublishTimeout   time.Duration
	ShutdownTimeout  time.Duration
	ReconnectMinimum time.Duration
	ReconnectMaximum time.Duration
	Logger           func(string, ...any)
}

type Runtime struct {
	connectionURL string
	processor     Processor
	options       RuntimeOptions
	logger        func(string, ...any)
}

func NewRuntime(connectionURL string, processor Processor, options RuntimeOptions) (*Runtime, error) {
	if connectionURL == "" || processor == nil {
		return nil, errors.New("business worker runtime requires RabbitMQ URL and processor")
	}
	if options.Prefetch < 1 || options.Prefetch > 100 {
		return nil, errors.New("business worker prefetch must be between 1 and 100")
	}
	if options.MaxRetries < 0 || options.MaxRetries > 20 || options.RetryDelay < time.Second || options.PublishTimeout <= 0 || options.ShutdownTimeout <= 0 {
		return nil, errors.New("business worker runtime options are invalid")
	}
	if options.ReconnectMinimum <= 0 || options.ReconnectMaximum < options.ReconnectMinimum {
		return nil, errors.New("business worker reconnect bounds are invalid")
	}
	logger := options.Logger
	if logger == nil {
		logger = log.Printf
	}
	return &Runtime{connectionURL: connectionURL, processor: processor, options: options, logger: logger}, nil
}

// Run maintains a single sequential consumer session. Broker/channel closure
// causes bounded jittered reconnection and topology/consumer recreation.
func (runtime *Runtime) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("business worker context is required")
	}
	attempt := uint32(0)
	for ctx.Err() == nil {
		session, err := openSession(ctx, runtime.connectionURL, runtime.options)
		if err != nil {
			attempt++
			runtime.logger("business worker connection unavailable; retrying")
			if err := waitContext(ctx, runtime.reconnectDelay(attempt)); err != nil {
				return nil
			}
			continue
		}
		attempt = 0
		handler, err := NewHandler(runtime.processor, session, HandlerOptions{
			MaxRetries: runtime.options.MaxRetries, PublishTimeout: runtime.options.PublishTimeout, Logger: runtime.logger,
		})
		if err != nil {
			_ = session.Close()
			return err
		}
		err = runtime.consumeSession(ctx, session, handler)
		_ = session.Close()
		if ctx.Err() != nil {
			return nil
		}
		attempt++
		if err != nil {
			runtime.logger("business worker session interrupted; reconnecting")
		}
		if err := waitContext(ctx, runtime.reconnectDelay(attempt)); err != nil {
			return nil
		}
	}
	return nil
}

func (runtime *Runtime) consumeSession(ctx context.Context, session *amqpSession, handler *Handler) error {
	for {
		select {
		case <-ctx.Done():
			_ = session.StopDeliveries()
			return nil
		case <-session.connectionClosed:
			return errors.New("RabbitMQ connection closed")
		case <-session.channelClosed:
			return errors.New("RabbitMQ channel closed")
		case delivery, ok := <-session.deliveries:
			if !ok {
				return errors.New("RabbitMQ delivery stream closed")
			}
			processingDone := make(chan error, 1)
			go func() { processingDone <- handler.Handle(context.Background(), delivery) }()
			select {
			case err := <-processingDone:
				if err != nil {
					return err
				}
			case <-ctx.Done():
				_ = session.StopDeliveries()
				timer := time.NewTimer(runtime.options.ShutdownTimeout)
				select {
				case <-processingDone:
					if !timer.Stop() {
						<-timer.C
					}
					return nil
				case <-timer.C:
					return nil
				}
			case <-session.connectionClosed:
				return errors.New("RabbitMQ connection closed during delivery")
			case <-session.channelClosed:
				return errors.New("RabbitMQ channel closed during delivery")
			}
		}
	}
}

func (runtime *Runtime) reconnectDelay(attempt uint32) time.Duration {
	delay := runtime.options.ReconnectMinimum
	for index := uint32(1); index < attempt && delay < runtime.options.ReconnectMaximum; index++ {
		if delay > runtime.options.ReconnectMaximum/2 {
			delay = runtime.options.ReconnectMaximum
			break
		}
		delay *= 2
	}
	if delay > runtime.options.ReconnectMaximum {
		delay = runtime.options.ReconnectMaximum
	}
	return jitter(delay)
}

type amqpSession struct {
	connection       *amqp.Connection
	channel          *amqp.Channel
	deliveries       <-chan amqp.Delivery
	confirmations    <-chan amqp.Confirmation
	returns          <-chan amqp.Return
	connectionClosed <-chan *amqp.Error
	channelClosed    <-chan *amqp.Error
	consumerTag      string
	publishMu        sync.Mutex
	closeOnce        sync.Once
}

func openSession(ctx context.Context, connectionURL string, options RuntimeOptions) (*amqpSession, error) {
	connection, err := dialRabbitMQ(ctx, connectionURL, 5*time.Second)
	if err != nil {
		return nil, err
	}
	session := &amqpSession{connection: connection, consumerTag: fmt.Sprintf("gopulse-business-worker-%d", time.Now().UnixNano())}
	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	session.channel = channel
	if err := platform.DeclareBusinessTopology(channel, options.RetryDelay); err != nil {
		_ = session.Close()
		return nil, err
	}
	if err := channel.Qos(options.Prefetch, 0, false); err != nil {
		_ = session.Close()
		return nil, err
	}
	if err := channel.Confirm(false); err != nil {
		_ = session.Close()
		return nil, err
	}
	session.confirmations = channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	session.returns = channel.NotifyReturn(make(chan amqp.Return, 1))
	session.channelClosed = channel.NotifyClose(make(chan *amqp.Error, 1))
	session.connectionClosed = connection.NotifyClose(make(chan *amqp.Error, 1))
	deliveries, err := channel.Consume(platform.BusinessQueue, session.consumerTag, false, false, false, false, nil)
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	session.deliveries = deliveries
	return session, nil
}

func (session *amqpSession) Publish(ctx context.Context, exchange, routingKey string, publishing amqp.Publishing) error {
	session.publishMu.Lock()
	defer session.publishMu.Unlock()
	if err := session.channel.PublishWithContext(ctx, exchange, routingKey, true, false, publishing); err != nil {
		return err
	}
	returned := false
	for {
		select {
		case message, ok := <-session.returns:
			if !ok {
				return errors.New("RabbitMQ return stream closed")
			}
			if message.MessageId == "" || message.MessageId == publishing.MessageId {
				returned = true
			}
		case confirmation, ok := <-session.confirmations:
			if !ok || !confirmation.Ack || returned {
				return errors.New("RabbitMQ secondary publish not confirmed")
			}
			// Mandatory returns precede confirms. A short defensive drain covers
			// scheduler ordering when both notifications are already readable.
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case message := <-session.returns:
				timer.Stop()
				if message.MessageId == "" || message.MessageId == publishing.MessageId {
					return errors.New("RabbitMQ secondary publish was unroutable")
				}
			case <-timer.C:
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-session.connectionClosed:
			return errors.New("RabbitMQ connection closed")
		case <-session.channelClosed:
			return errors.New("RabbitMQ channel closed")
		}
	}
}

func (session *amqpSession) StopDeliveries() error {
	if session.channel == nil {
		return nil
	}
	return session.channel.Cancel(session.consumerTag, false)
}

func (session *amqpSession) Close() error {
	var result error
	session.closeOnce.Do(func() {
		if session.channel != nil {
			if err := session.channel.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
				result = err
			}
		}
		if session.connection != nil {
			if err := session.connection.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) && result == nil {
				result = err
			}
		}
	})
	return result
}

func dialRabbitMQ(ctx context.Context, connectionURL string, timeout time.Duration) (*amqp.Connection, error) {
	dialer := &net.Dialer{Timeout: timeout}
	return amqp.DialConfig(connectionURL, amqp.Config{Dial: func(network, address string) (net.Conn, error) {
		connection, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
			_ = connection.Close()
			return nil, err
		}
		return connection, nil
	}})
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func jitter(base time.Duration) time.Duration {
	if base <= 1 {
		return base
	}
	spread := base / 4
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(spread*2+1)))
	if err != nil {
		return base
	}
	return base - spread + time.Duration(value.Int64())
}
