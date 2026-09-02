package platform

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	amqp "github.com/rabbitmq/amqp091-go"
)

type exchangeDeclaration struct {
	name, kind                            string
	durable, autoDelete, internal, noWait bool
	arguments                             amqp.Table
}

type queueDeclaration struct {
	name                                   string
	durable, autoDelete, exclusive, noWait bool
	arguments                              amqp.Table
}

type queueBinding struct {
	name, key, exchange string
	noWait              bool
	arguments           amqp.Table
}

type fakeTopologyChannel struct {
	exchanges []exchangeDeclaration
	queues    []queueDeclaration
	bindings  []queueBinding
	failOn    string
}

func (channel *fakeTopologyChannel) ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, arguments amqp.Table) error {
	if channel.failOn == name {
		return errors.New("inequivalent declaration")
	}
	channel.exchanges = append(channel.exchanges, exchangeDeclaration{name, kind, durable, autoDelete, internal, noWait, arguments})
	return nil
}

func (channel *fakeTopologyChannel) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, arguments amqp.Table) (amqp.Queue, error) {
	if channel.failOn == name {
		return amqp.Queue{}, errors.New("inequivalent declaration")
	}
	channel.queues = append(channel.queues, queueDeclaration{name, durable, autoDelete, exclusive, noWait, arguments})
	return amqp.Queue{Name: name}, nil
}

func (channel *fakeTopologyChannel) QueueBind(name, key, exchange string, noWait bool, arguments amqp.Table) error {
	channel.bindings = append(channel.bindings, queueBinding{name, key, exchange, noWait, arguments})
	return nil
}

func TestDeclareBusinessTopologyIsDurableVersionedAndRepeatable(t *testing.T) {
	channel := &fakeTopologyChannel{}
	for range 2 {
		if err := DeclareBusinessTopology(channel, 30*time.Second); err != nil {
			t.Fatalf("DeclareBusinessTopology() error = %v", err)
		}
	}
	if len(channel.exchanges) != 6 || len(channel.queues) != 6 || len(channel.bindings) != 12 {
		t.Fatalf("declaration counts exchanges=%d queues=%d bindings=%d", len(channel.exchanges), len(channel.queues), len(channel.bindings))
	}
	for _, declaration := range channel.exchanges {
		if declaration.kind != amqp.ExchangeDirect || !declaration.durable || declaration.autoDelete || declaration.internal || declaration.noWait || declaration.arguments != nil {
			t.Fatalf("exchange declaration = %#v", declaration)
		}
	}
	for _, declaration := range channel.queues {
		if !declaration.durable || declaration.autoDelete || declaration.exclusive || declaration.noWait {
			t.Fatalf("queue declaration = %#v", declaration)
		}
	}

	wantRetryArguments := amqp.Table{"x-message-ttl": int64(30000), "x-dead-letter-exchange": BusinessExchange}
	if !reflect.DeepEqual(channel.queues[1].arguments, wantRetryArguments) || !reflect.DeepEqual(channel.queues[4].arguments, wantRetryArguments) {
		t.Fatalf("retry arguments first=%#v second=%#v", channel.queues[1].arguments, channel.queues[4].arguments)
	}
	wantBindings := []queueBinding{
		{name: BusinessQueue, key: bus.CommentCreatedRoutingKey, exchange: BusinessExchange},
		{name: BusinessRetryQueue, key: bus.CommentCreatedRoutingKey, exchange: BusinessRetryExchange},
		{name: BusinessDeadQueue, key: bus.CommentCreatedRoutingKey, exchange: BusinessDeadExchange},
		{name: BusinessQueue, key: bus.PostLikedRoutingKey, exchange: BusinessExchange},
		{name: BusinessRetryQueue, key: bus.PostLikedRoutingKey, exchange: BusinessRetryExchange},
		{name: BusinessDeadQueue, key: bus.PostLikedRoutingKey, exchange: BusinessDeadExchange},
	}
	if !reflect.DeepEqual(channel.bindings[:6], wantBindings) || !reflect.DeepEqual(channel.bindings[6:], wantBindings) {
		t.Fatalf("bindings = %#v", channel.bindings)
	}
}

func TestDeclareBusinessTopologyRejectsInvalidConfigurationAndDeclarationMismatch(t *testing.T) {
	if err := DeclareBusinessTopology(nil, 30*time.Second); err == nil {
		t.Fatal("nil channel error = nil")
	}
	for _, retryDelay := range []time.Duration{0, 25 * time.Hour} {
		if err := DeclareBusinessTopology(&fakeTopologyChannel{}, retryDelay); err == nil {
			t.Fatalf("retry delay %s error = nil", retryDelay)
		}
	}
	channel := &fakeTopologyChannel{failOn: BusinessRetryQueue}
	err := DeclareBusinessTopology(channel, 30*time.Second)
	if err == nil || !strings.Contains(err.Error(), BusinessRetryQueue) || !strings.Contains(err.Error(), "inequivalent declaration") {
		t.Fatalf("declaration mismatch error = %v", err)
	}
}
