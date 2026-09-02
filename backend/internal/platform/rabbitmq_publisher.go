package platform

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	defaultRabbitMQPublisherRetryDelay = 30 * time.Second
	defaultRabbitMQDialTimeout         = 5 * time.Second
	minimumRabbitMQDialTimeout         = 100 * time.Millisecond
	maximumRabbitMQDialTimeout         = 30 * time.Second

	defaultRabbitMQReturnDrainTimeout = 10 * time.Millisecond
	minimumRabbitMQReturnDrainTimeout = time.Millisecond
	maximumRabbitMQReturnDrainTimeout = time.Second
	minimumRabbitMQRetryBackoff       = 100 * time.Millisecond
	maximumRabbitMQRetryBackoff       = 5 * time.Minute
)

// RabbitMQChannel is the small AMQP channel surface used by the publisher.
// Keeping it as an interface lets the confirm/return state machine be tested
// without requiring a live broker.
type RabbitMQChannel interface {
	AMQPTopologyDeclarer
	Confirm(bool) error
	PublishWithContext(context.Context, string, string, bool, bool, amqp.Publishing) error
	NotifyPublish(chan amqp.Confirmation) chan amqp.Confirmation
	NotifyReturn(chan amqp.Return) chan amqp.Return
	NotifyClose(chan *amqp.Error) chan *amqp.Error
	Close() error
}

// RabbitMQConnection is the small AMQP connection surface used by the
// publisher. OpenChannel deliberately returns the interface above so tests can
// inject a deterministic channel implementation.
type RabbitMQConnection interface {
	OpenChannel() (RabbitMQChannel, error)
	NotifyClose(chan *amqp.Error) chan *amqp.Error
	Close() error
}

// RabbitMQDialer is injectable for unit tests. Production callers should leave
// RabbitMQPublisherOptions.Dialer unset so the amqp091-go dialer is used.
type RabbitMQDialer func(context.Context, string, time.Duration) (RabbitMQConnection, error)

// RabbitMQPublisherOptions controls connection establishment and retry timing.
// Clock, Jitter, Wait, and Dialer are intentionally optional test seams; they
// do not alter the production topology or delivery properties.
type RabbitMQPublisherOptions struct {
	RetryDelay         time.Duration
	DialTimeout        time.Duration
	ReturnDrainTimeout time.Duration
	Dialer             RabbitMQDialer
	Clock              func() time.Time
	Jitter             func(time.Duration) time.Duration
	Wait               func(context.Context, time.Duration) error
}

type RabbitMQPublisher struct {
	connectionURL string
	retryDelay    time.Duration
	dialTimeout   time.Duration

	returnDrainTimeout time.Duration
	dialer             RabbitMQDialer
	clock              func() time.Time
	jitter             func(time.Duration) time.Duration
	wait               func(context.Context, time.Duration) error

	stateMu      sync.Mutex
	closed       bool
	state        *rabbitMQPublisherState
	retryAttempt uint32
	nextRetryAt  time.Time

	// A single in-flight publish makes the publisher-confirm and mandatory
	// return correlation deterministic: the next confirmation belongs to the
	// message currently being awaited.
	publishMu sync.Mutex
}

type rabbitMQPublisherState struct {
	connection    RabbitMQConnection
	channel       RabbitMQChannel
	confirmations <-chan amqp.Confirmation
	returns       <-chan amqp.Return
	channelClosed <-chan *amqp.Error
	closed        <-chan *amqp.Error
}

type amqpConnectionAdapter struct {
	connection *amqp.Connection
}

func (adapter *amqpConnectionAdapter) OpenChannel() (RabbitMQChannel, error) {
	channel, err := adapter.connection.Channel()
	if err != nil {
		return nil, err
	}
	return channel, nil
}

func (adapter *amqpConnectionAdapter) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	return adapter.connection.NotifyClose(receiver)
}

func (adapter *amqpConnectionAdapter) Close() error {
	return adapter.connection.Close()
}

// NewRabbitMQPublisher creates a lazy publisher. It validates only the URL;
// the network connection, channel, confirm listeners, and durable topology are
// established on the first Publish.
func NewRabbitMQPublisher(connectionURL string, options ...RabbitMQPublisherOptions) (*RabbitMQPublisher, error) {
	parsed, err := url.Parse(connectionURL)
	if err != nil || (parsed.Scheme != "amqp" && parsed.Scheme != "amqps") || parsed.Host == "" {
		return nil, errors.New("create RabbitMQ publisher: invalid AMQP URL")
	}
	publisherOptions := RabbitMQPublisherOptions{
		RetryDelay:         defaultRabbitMQPublisherRetryDelay,
		DialTimeout:        defaultRabbitMQDialTimeout,
		ReturnDrainTimeout: defaultRabbitMQReturnDrainTimeout,
		Dialer:             defaultRabbitMQDial,
		Clock:              time.Now,
		Jitter:             rabbitMQJitter,
		Wait:               waitWithContext,
	}
	if len(options) > 0 {
		if options[0].RetryDelay != 0 {
			publisherOptions.RetryDelay = options[0].RetryDelay
		}
		if options[0].DialTimeout != 0 {
			publisherOptions.DialTimeout = options[0].DialTimeout
		}
		if options[0].ReturnDrainTimeout != 0 {
			publisherOptions.ReturnDrainTimeout = options[0].ReturnDrainTimeout
		}
		if options[0].Dialer != nil {
			publisherOptions.Dialer = options[0].Dialer
		}
		if options[0].Clock != nil {
			publisherOptions.Clock = options[0].Clock
		}
		if options[0].Jitter != nil {
			publisherOptions.Jitter = options[0].Jitter
		}
		if options[0].Wait != nil {
			publisherOptions.Wait = options[0].Wait
		}
	}
	if publisherOptions.RetryDelay < minimumBusinessRetryDelay || publisherOptions.RetryDelay > maximumBusinessRetryDelay {
		return nil, errors.New("create RabbitMQ publisher: retry delay is outside the supported range")
	}
	if publisherOptions.DialTimeout < minimumRabbitMQDialTimeout || publisherOptions.DialTimeout > maximumRabbitMQDialTimeout {
		return nil, errors.New("create RabbitMQ publisher: dial timeout is outside the supported range")
	}
	if publisherOptions.ReturnDrainTimeout < minimumRabbitMQReturnDrainTimeout || publisherOptions.ReturnDrainTimeout > maximumRabbitMQReturnDrainTimeout {
		return nil, errors.New("create RabbitMQ publisher: return drain timeout is outside the supported range")
	}
	return &RabbitMQPublisher{
		connectionURL:      connectionURL,
		retryDelay:         publisherOptions.RetryDelay,
		dialTimeout:        publisherOptions.DialTimeout,
		returnDrainTimeout: publisherOptions.ReturnDrainTimeout,
		dialer:             publisherOptions.Dialer,
		clock:              publisherOptions.Clock,
		jitter:             publisherOptions.Jitter,
		wait:               publisherOptions.Wait,
	}, nil
}

func (publisher *RabbitMQPublisher) Publish(ctx context.Context, envelope bus.Envelope) error {
	if ctx == nil {
		return outbox.NewPublishError(outbox.FailureInternal, errors.New("publish context is required"))
	}
	routingKey, err := envelope.RoutingKey()
	if err != nil {
		return outbox.NewPublishError(outbox.FailureInternal, err)
	}
	metadata, err := envelope.Metadata()
	if err != nil {
		return outbox.NewPublishError(outbox.FailureInternal, err)
	}
	body, err := bus.Encode(envelope)
	if err != nil {
		return outbox.NewPublishError(outbox.FailureInternal, err)
	}

	publisher.publishMu.Lock()
	defer publisher.publishMu.Unlock()

	state, err := publisher.ensureState(ctx)
	if err != nil {
		return err
	}
	publishing := amqp.Publishing{
		Headers:      amqp.Table{},
		ContentType:  metadata.ContentType,
		Type:         metadata.Type,
		DeliveryMode: amqp.Persistent,
		MessageId:    metadata.MessageID,
		Timestamp:    metadata.Timestamp,
		Body:         body,
	}
	if err := state.channel.PublishWithContext(ctx, BusinessExchange, routingKey, true, false, publishing); err != nil {
		publisher.resetState(state, true)
		return classifyRabbitMQPublishError(ctx, err)
	}

	return publisher.awaitPublishResult(ctx, state, metadata.MessageID)
}

func (publisher *RabbitMQPublisher) awaitPublishResult(ctx context.Context, state *rabbitMQPublisherState, messageID string) error {
	returned := false
	for {
		select {
		case message, ok := <-state.returns:
			if !ok {
				publisher.resetState(state, true)
				return outbox.NewPublishError(outbox.FailurePublisherClosed, nil)
			}
			if rabbitMQReturnMatches(message, messageID) {
				returned = true
			}
		case confirmation, ok := <-state.confirmations:
			if !ok {
				publisher.resetState(state, true)
				return outbox.NewPublishError(outbox.FailurePublisherClosed, nil)
			}
			if !confirmation.Ack {
				publisher.resetState(state, true)
				if returned {
					return outbox.NewPublishError(outbox.FailurePublishUnroutable, nil)
				}
				return outbox.NewPublishError(outbox.FailurePublishNack, nil)
			}
			// RabbitMQ sends a mandatory return before the corresponding
			// publisher confirmation. Drain briefly after the ack as a defensive
			// guard against the two notifications becoming ready together in the
			// client scheduler; never mark an unroutable message successful.
			drainedReturn, err := publisher.drainReturns(ctx, state, messageID)
			if err != nil {
				return err
			}
			if returned || drainedReturn {
				return outbox.NewPublishError(outbox.FailurePublishUnroutable, nil)
			}
			return nil
		case <-ctx.Done():
			publisher.resetState(state, true)
			return classifyRabbitMQPublishError(ctx, ctx.Err())
		case <-state.channelClosed:
			publisher.resetState(state, true)
			return outbox.NewPublishError(outbox.FailurePublishUnavailable, nil)
		case <-state.closed:
			publisher.resetState(state, true)
			return outbox.NewPublishError(outbox.FailurePublishUnavailable, nil)
		}
	}
}

func (publisher *RabbitMQPublisher) drainReturns(ctx context.Context, state *rabbitMQPublisherState, messageID string) (bool, error) {
	timer := time.NewTimer(publisher.returnDrainTimeout)
	defer timer.Stop()
	for {
		select {
		case message, ok := <-state.returns:
			if !ok {
				publisher.resetState(state, true)
				return false, outbox.NewPublishError(outbox.FailurePublisherClosed, nil)
			}
			if rabbitMQReturnMatches(message, messageID) {
				return true, nil
			}
		case <-timer.C:
			return false, nil
		case <-ctx.Done():
			publisher.resetState(state, true)
			return false, classifyRabbitMQPublishError(ctx, ctx.Err())
		case <-state.channelClosed:
			publisher.resetState(state, true)
			return false, outbox.NewPublishError(outbox.FailurePublishUnavailable, nil)
		case <-state.closed:
			publisher.resetState(state, true)
			return false, outbox.NewPublishError(outbox.FailurePublishUnavailable, nil)
		}
	}
}

func rabbitMQReturnMatches(message amqp.Return, messageID string) bool {
	return message.MessageId == "" || message.MessageId == messageID
}

func (publisher *RabbitMQPublisher) ensureState(ctx context.Context) (*rabbitMQPublisherState, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, classifyRabbitMQPublishError(ctx, err)
		}
		publisher.stateMu.Lock()
		if publisher.closed {
			publisher.stateMu.Unlock()
			return nil, outbox.NewPublishError(outbox.FailurePublisherClosed, nil)
		}
		if publisher.state != nil {
			state := publisher.state
			publisher.stateMu.Unlock()
			return state, nil
		}
		waitDuration := publisher.retryWaitLocked(publisher.clock())
		wait := publisher.wait
		publisher.stateMu.Unlock()

		if waitDuration > 0 {
			if err := wait(ctx, waitDuration); err != nil {
				return nil, classifyRabbitMQPublishError(ctx, err)
			}
			continue
		}

		connection, err := publisher.dialer(ctx, publisher.connectionURL, publisher.dialTimeout)
		if err != nil {
			if ctx.Err() == nil {
				publisher.noteConnectionFailure()
			}
			return nil, classifyRabbitMQPublishError(ctx, err)
		}
		state, err := publisher.buildState(connection)
		if err != nil {
			closeRabbitMQState(state)
			if ctx.Err() == nil {
				publisher.noteConnectionFailure()
			}
			return nil, outbox.NewPublishError(outbox.FailurePublishUnavailable, err)
		}

		publisher.stateMu.Lock()
		if publisher.closed {
			publisher.stateMu.Unlock()
			closeRabbitMQState(state)
			return nil, outbox.NewPublishError(outbox.FailurePublisherClosed, nil)
		}
		if publisher.state != nil {
			existing := publisher.state
			publisher.stateMu.Unlock()
			closeRabbitMQState(state)
			return existing, nil
		}
		publisher.state = state
		publisher.retryAttempt = 0
		publisher.nextRetryAt = time.Time{}
		publisher.stateMu.Unlock()
		return state, nil
	}
}

func (publisher *RabbitMQPublisher) buildState(connection RabbitMQConnection) (*rabbitMQPublisherState, error) {
	if connection == nil {
		return nil, errors.New("RabbitMQ dialer returned a nil connection")
	}
	channel, err := connection.OpenChannel()
	if err != nil {
		return &rabbitMQPublisherState{connection: connection}, err
	}
	state := &rabbitMQPublisherState{connection: connection, channel: channel}
	if channel == nil {
		return state, errors.New("RabbitMQ connection returned a nil channel")
	}
	if err := channel.Confirm(false); err != nil {
		return state, fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}
	state.confirmations = channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	state.returns = channel.NotifyReturn(make(chan amqp.Return, 1))
	state.channelClosed = channel.NotifyClose(make(chan *amqp.Error, 1))
	state.closed = connection.NotifyClose(make(chan *amqp.Error, 1))
	if err := DeclareBusinessTopology(channel, publisher.retryDelay); err != nil {
		return state, err
	}
	return state, nil
}

func (publisher *RabbitMQPublisher) dial(ctx context.Context) (RabbitMQConnection, error) {
	return publisher.dialer(ctx, publisher.connectionURL, publisher.dialTimeout)
}

func (publisher *RabbitMQPublisher) retryWaitLocked(now time.Time) time.Duration {
	if publisher.nextRetryAt.IsZero() || !publisher.nextRetryAt.After(now) {
		return 0
	}
	return publisher.nextRetryAt.Sub(now)
}

func (publisher *RabbitMQPublisher) noteConnectionFailure() {
	publisher.stateMu.Lock()
	defer publisher.stateMu.Unlock()
	publisher.retryAttempt++
	backoff := publisher.retryBackoff(publisher.retryAttempt)
	publisher.nextRetryAt = publisher.clock().Add(backoff)
}

func (publisher *RabbitMQPublisher) retryBackoff(attempt uint32) time.Duration {
	backoff := publisher.retryDelay
	for index := uint32(1); index < attempt; index++ {
		if backoff >= maximumRabbitMQRetryBackoff/2 {
			backoff = maximumRabbitMQRetryBackoff
			break
		}
		backoff *= 2
	}
	if backoff > maximumRabbitMQRetryBackoff {
		backoff = maximumRabbitMQRetryBackoff
	}
	jittered := publisher.jitter(backoff)
	if jittered < minimumRabbitMQRetryBackoff {
		return minimumRabbitMQRetryBackoff
	}
	if jittered > maximumRabbitMQRetryBackoff {
		return maximumRabbitMQRetryBackoff
	}
	return jittered
}

// resetState is called while publishMu is held. It atomically removes the
// current state before closing its listeners so a later publish cannot consume
// stale confirmations or returns from a broken channel.
func (publisher *RabbitMQPublisher) resetState(state *rabbitMQPublisherState, retry bool) {
	if state == nil {
		return
	}
	publisher.stateMu.Lock()
	if publisher.state != state {
		publisher.stateMu.Unlock()
		return
	}
	publisher.state = nil
	if retry && !publisher.closed {
		publisher.retryAttempt++
		publisher.nextRetryAt = publisher.clock().Add(publisher.retryBackoff(publisher.retryAttempt))
	}
	publisher.stateMu.Unlock()
	closeRabbitMQState(state)
}

func (publisher *RabbitMQPublisher) Close() error {
	publisher.publishMu.Lock()
	defer publisher.publishMu.Unlock()

	publisher.stateMu.Lock()
	if publisher.closed {
		publisher.stateMu.Unlock()
		return nil
	}
	publisher.closed = true
	state := publisher.state
	publisher.state = nil
	publisher.stateMu.Unlock()
	if state == nil {
		return nil
	}
	return closeRabbitMQState(state)
}

func closeRabbitMQState(state *rabbitMQPublisherState) error {
	if state == nil {
		return nil
	}
	var firstErr error
	if state.channel != nil {
		if err := state.channel.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
			firstErr = err
		}
	}
	if state.connection != nil {
		if err := state.connection.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func classifyRabbitMQPublishError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return outbox.NewPublishError(outbox.FailurePublishTimeout, err)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return outbox.NewPublishError(outbox.FailurePublisherClosed, err)
	}
	if err == nil {
		return outbox.NewPublishError(outbox.FailurePublishUnavailable, nil)
	}
	return outbox.NewPublishError(outbox.FailurePublishUnavailable, fmt.Errorf("AMQP publish failed: %w", err))
}

func defaultRabbitMQDial(ctx context.Context, connectionURL string, dialTimeout time.Duration) (RabbitMQConnection, error) {
	dialer := &net.Dialer{Timeout: dialTimeout}
	amqpConfig := amqp.Config{
		Dial: func(network, address string) (net.Conn, error) {
			connection, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			// amqp091-go clears this deadline after the AMQP handshake. It is
			// intentionally derived from the publisher dial timeout rather than
			// the business publish context, so a successful connection cannot
			// inherit a short publish deadline.
			handshakeTimeout := dialTimeout
			if deadline, ok := ctx.Deadline(); ok {
				if remaining := time.Until(deadline); remaining > 0 && remaining < handshakeTimeout {
					handshakeTimeout = remaining
				}
			}
			if err := connection.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
				_ = connection.Close()
				return nil, err
			}
			return connection, nil
		},
	}
	connection, err := amqp.DialConfig(connectionURL, amqpConfig)
	if err != nil {
		return nil, err
	}
	return &amqpConnectionAdapter{connection: connection}, nil
}

func waitWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func rabbitMQJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	spread := base / 2
	if spread == 0 {
		return base
	}
	maximum := big.NewInt(int64(spread*2 + 1))
	randomValue, err := cryptorand.Int(cryptorand.Reader, maximum)
	if err != nil {
		return base
	}
	return base - spread + time.Duration(randomValue.Int64())
}
