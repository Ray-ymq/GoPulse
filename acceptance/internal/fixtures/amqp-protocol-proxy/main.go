// Command amqp-protocol-proxy is an acceptance-only AMQP 0-9-1 frame proxy.
// It forwards the real broker protocol and drops exactly one publisher confirm
// or consumer acknowledgement for a selected event.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	modePublisherConfirm = "publisher-confirm"
	modeConsumerAck      = "consumer-ack"
	frameEnd             = 0xce
	maxFramePayload      = 64 << 20
	publisherTargetWait  = 30 * time.Second
)

type status struct {
	Mode                     string `json:"mode"`
	TargetEvent              string `json:"target_event,omitempty"`
	PublisherReceived        uint64 `json:"publisher_received"`
	PublisherEventReceived   uint64 `json:"publisher_event_received"`
	PublisherConfirmed       uint64 `json:"publisher_confirmed"`
	PublisherConfirmDropped  uint64 `json:"publisher_confirm_dropped"`
	ConsumerDeliveries       uint64 `json:"consumer_deliveries"`
	ConsumerDeliveryReceived uint64 `json:"consumer_delivery_received"`
	ConsumerAckObserved      uint64 `json:"consumer_ack_observed"`
	ConsumerAckDropped       uint64 `json:"consumer_ack_dropped"`
	ConsumerRedelivered      uint64 `json:"consumer_redelivered"`
	ConsumerRedeliveryAck    uint64 `json:"consumer_redelivery_ack"`
	Disconnects              uint64 `json:"disconnects"`
	LastEvent                string `json:"last_event,omitempty"`
}

type store struct {
	mu          sync.Mutex
	value       status
	targetReady chan struct{}
	targetOnce  sync.Once
}

func (state *store) update(update func(*status)) {
	state.mu.Lock()
	defer state.mu.Unlock()
	update(&state.value)
}

func (state *store) snapshot() status {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.value
}

func (state *store) observePublisherConfirm(eventID string) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.value.PublisherConfirmed++
	if eventID == "" || state.value.TargetEvent == "" || state.value.PublisherConfirmDropped > 0 || eventID != state.value.TargetEvent {
		return false
	}
	state.value.PublisherConfirmDropped++
	state.value.LastEvent = eventID
	return true
}

func (state *store) observeConsumerAck(redelivered bool) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.value.ConsumerAckObserved++
	if state.value.ConsumerAckDropped == 0 {
		state.value.ConsumerAckDropped++
		return true
	}
	if redelivered {
		state.value.ConsumerRedeliveryAck++
	}
	return false
}

func (state *store) setTarget(eventID string) error {
	if eventID == "" || len(eventID) > 128 {
		return errors.New("event_id is required and bounded")
	}
	state.mu.Lock()
	if state.targetReady == nil {
		state.targetReady = make(chan struct{})
	}
	ready := state.targetReady
	state.value.TargetEvent = eventID
	state.mu.Unlock()
	state.targetOnce.Do(func() { close(ready) })
	return nil
}

func (state *store) waitForTarget(timeout time.Duration) string {
	state.mu.Lock()
	if state.value.TargetEvent != "" {
		target := state.value.TargetEvent
		state.mu.Unlock()
		return target
	}
	if state.targetReady == nil {
		state.targetReady = make(chan struct{})
	}
	ready := state.targetReady
	state.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ready:
		state.mu.Lock()
		target := state.value.TargetEvent
		state.mu.Unlock()
		return target
	case <-timer.C:
		return ""
	}
}

type frame struct {
	kind    byte
	channel uint16
	payload []byte
}

func main() {
	listen := flag.String("listen", "0.0.0.0:5672", "AMQP proxy listener")
	control := flag.String("control", "127.0.0.1:19126", "status HTTP listener")
	upstream := flag.String("upstream", "", "upstream AMQP address")
	mode := flag.String("mode", modePublisherConfirm, "publisher-confirm or consumer-ack")
	flag.Parse()
	if *upstream == "" || (*mode != modePublisherConfirm && *mode != modeConsumerAck) {
		fmt.Fprintln(os.Stderr, "upstream and a supported mode are required")
		os.Exit(2)
	}
	state := &store{value: status{Mode: *mode}}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = listener.Close() }()
	controlListener, err := net.Listen("tcp", *control)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = controlListener.Close() }()
	controlServer := &http.Server{Addr: *control, Handler: controlHandler(state), ReadHeaderTimeout: 2 * time.Second}
	go func() { _ = controlServer.Serve(controlListener) }()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = controlServer.Shutdown(shutdown)
	}()
	for {
		client, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		go proxy(client, *upstream, *mode, state)
	}
}

func controlHandler(state *store) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ready":
			writer.WriteHeader(http.StatusOK)
		case "/status":
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(state.snapshot())
		case "/target":
			values, err := url.ParseQuery(request.URL.RawQuery)
			if err != nil {
				http.Error(writer, "invalid target", http.StatusBadRequest)
				return
			}
			if err := state.setTarget(values.Get("event_id")); err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	})
}

func proxy(client net.Conn, upstreamAddress, mode string, state *store) {
	defer func() { _ = client.Close() }()
	upstream, err := net.DialTimeout("tcp", upstreamAddress, 5*time.Second)
	if err != nil {
		return
	}
	defer func() { _ = upstream.Close() }()
	closed := make(chan struct{})
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			state.update(func(value *status) { value.Disconnects++ })
			close(closed)
			_ = client.Close()
			_ = upstream.Close()
		})
	}
	connection := &connectionState{mode: mode, state: state, closeBoth: closeBoth}
	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); clientToServer(client, upstream, connection) }()
	go func() { defer wait.Done(); serverToClient(upstream, client, connection) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Minute):
		closeBoth()
	}
	wait.Wait()
}

type pendingDelivery struct {
	channel     uint16
	deliveryTag uint64
	redelivered bool
	remaining   uint64
	body        []byte
}

type connectionState struct {
	mode              string
	state             *store
	closeBoth         func()
	mu                sync.Mutex
	consumerChannels  map[uint16]bool
	pendingDeliveries map[uint16]*pendingDelivery
	pendingPublishes  map[uint16]uint64
	publishBodies     map[uint16]map[uint64]string
	publishInFlight   map[uint16]*pendingPublish
	targetDelivery    map[uint16]uint64
	targetRedelivered map[uint16]bool
}

type pendingPublish struct {
	sequence  uint64
	remaining uint64
	body      []byte
}

func (connection *connectionState) init() {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.consumerChannels == nil {
		connection.consumerChannels = map[uint16]bool{}
		connection.pendingDeliveries = map[uint16]*pendingDelivery{}
		connection.pendingPublishes = map[uint16]uint64{}
		connection.publishBodies = map[uint16]map[uint64]string{}
		connection.publishInFlight = map[uint16]*pendingPublish{}
		connection.targetDelivery = map[uint16]uint64{}
		connection.targetRedelivered = map[uint16]bool{}
	}
}

func clientToServer(client, upstream net.Conn, connection *connectionState) {
	connection.init()
	if err := copyProtocolHeader(client, upstream); err != nil {
		connection.closeBoth()
		return
	}
	for {
		value, err := readFrame(client)
		if err != nil {
			connection.closeBoth()
			return
		}
		if value.kind == 1 {
			connection.inspectClientMethod(value)
		}
		if value.kind == 2 {
			connection.inspectClientHeader(value)
		}
		if value.kind == 3 {
			connection.inspectClientBody(value)
		}
		if connection.shouldDropClientFrame(value) {
			connection.closeBoth()
			return
		}
		if err := writeFrame(upstream, value); err != nil {
			connection.closeBoth()
			return
		}
	}
}

func serverToClient(upstream, client net.Conn, connection *connectionState) {
	connection.init()
	for {
		value, err := readFrame(upstream)
		if err != nil {
			connection.closeBoth()
			return
		}
		if value.kind == 1 {
			connection.inspectServerMethod(value)
		}
		if value.kind == 2 {
			connection.inspectServerHeader(value)
		}
		if value.kind == 3 {
			connection.inspectServerBody(value)
		}
		if connection.shouldDropServerFrame(value) {
			connection.closeBoth()
			return
		}
		if err := writeFrame(client, value); err != nil {
			connection.closeBoth()
			return
		}
	}
}

func copyProtocolHeader(reader io.Reader, writer io.Writer) error {
	header := make([]byte, 8)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	_, err := writer.Write(header)
	return err
}

func (connection *connectionState) inspectClientMethod(value frame) {
	if len(value.payload) < 4 {
		return
	}
	classID, methodID := binary.BigEndian.Uint16(value.payload[0:2]), binary.BigEndian.Uint16(value.payload[2:4])
	if classID == 60 && methodID == 20 && len(value.payload) >= 7 {
		queue, _, ok := readShortString(value.payload[6:])
		if ok && strings.Contains(queue, "search-indexer") {
			connection.mu.Lock()
			connection.consumerChannels[value.channel] = true
			connection.mu.Unlock()
		}
	}
	if classID == 60 && methodID == 40 {
		connection.mu.Lock()
		connection.pendingPublishes[value.channel]++
		connection.mu.Unlock()
		connection.state.update(func(state *status) { state.PublisherReceived++ })
	}
}

func (connection *connectionState) inspectClientBody(value frame) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	pending := connection.publishInFlight[value.channel]
	if pending == nil || pending.remaining == 0 {
		return
	}
	pending.body = appendBounded(pending.body, value.payload, 1<<20)
	if uint64(len(value.payload)) >= pending.remaining {
		pending.remaining = 0
		if connection.publishBodies[value.channel] == nil {
			connection.publishBodies[value.channel] = map[uint64]string{}
		}
		eventID := eventIDFromBody(pending.body)
		connection.publishBodies[value.channel][pending.sequence] = eventID
		if eventID != "" {
			connection.state.update(func(state *status) {
				state.PublisherEventReceived++
				state.LastEvent = eventID
			})
		}
		delete(connection.publishInFlight, value.channel)
	} else {
		pending.remaining -= uint64(len(value.payload))
	}
}

func (connection *connectionState) inspectClientHeader(value frame) {
	if len(value.payload) < 14 {
		return
	}
	bodySize := binary.BigEndian.Uint64(value.payload[4:12])
	connection.mu.Lock()
	defer connection.mu.Unlock()
	sequence := connection.pendingPublishes[value.channel]
	if sequence == 0 {
		return
	}
	connection.publishInFlight[value.channel] = &pendingPublish{sequence: sequence, remaining: bodySize}
	if bodySize == 0 {
		if connection.publishBodies[value.channel] == nil {
			connection.publishBodies[value.channel] = map[uint64]string{}
		}
		connection.publishBodies[value.channel][sequence] = ""
		delete(connection.publishInFlight, value.channel)
	}
}

func (connection *connectionState) inspectServerMethod(value frame) {
	if len(value.payload) < 4 {
		return
	}
	classID, methodID := binary.BigEndian.Uint16(value.payload[0:2]), binary.BigEndian.Uint16(value.payload[2:4])
	if classID == 60 && methodID == 60 {
		connection.mu.Lock()
		defer connection.mu.Unlock()
		if !connection.consumerChannels[value.channel] || len(value.payload) < 5 {
			return
		}
		_, offset, ok := readShortString(value.payload[4:])
		if !ok || len(value.payload) < 4+offset+9 {
			return
		}
		deliveryTag := binary.BigEndian.Uint64(value.payload[4+offset : 4+offset+8])
		redelivered := value.payload[4+offset+8] != 0
		connection.pendingDeliveries[value.channel] = &pendingDelivery{channel: value.channel, deliveryTag: deliveryTag, redelivered: redelivered}
	}
}

func (connection *connectionState) inspectServerHeader(value frame) {
	if len(value.payload) < 14 {
		return
	}
	bodySize := binary.BigEndian.Uint64(value.payload[4:12])
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if pending := connection.pendingDeliveries[value.channel]; pending != nil {
		pending.remaining = bodySize
		if bodySize == 0 {
			connection.finishDeliveryLocked(pending)
			delete(connection.pendingDeliveries, value.channel)
		}
		return
	}
	// Publisher confirms use the publish sequence number. The body is parsed
	// before the confirm arrives, so the sequence maps to the exact event.
	if connection.mode == modePublisherConfirm {
		sequence := connection.pendingPublishes[value.channel]
		if sequence == 0 {
			return
		}
		if connection.publishBodies[value.channel] == nil {
			connection.publishBodies[value.channel] = map[uint64]string{}
		}
		connection.publishBodies[value.channel][sequence] = ""
	}
}

func (connection *connectionState) inspectServerBody(value frame) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if pending := connection.pendingDeliveries[value.channel]; pending != nil && pending.remaining > 0 {
		pending.body = appendBounded(pending.body, value.payload, 1<<20)
		if uint64(len(value.payload)) >= pending.remaining {
			pending.remaining = 0
			connection.finishDeliveryLocked(pending)
			delete(connection.pendingDeliveries, value.channel)
		} else {
			pending.remaining -= uint64(len(value.payload))
		}
	}
}

func (connection *connectionState) finishDeliveryLocked(pending *pendingDelivery) {
	eventID := eventIDFromBody(pending.body)
	connection.state.update(func(state *status) {
		state.ConsumerDeliveries++
		if eventID != "" {
			state.LastEvent = eventID
		}
		if pending.redelivered {
			state.ConsumerRedelivered++
		}
		if state.TargetEvent != "" && eventID == state.TargetEvent {
			state.ConsumerDeliveryReceived++
			connection.targetDelivery[pending.channel] = pending.deliveryTag
			connection.targetRedelivered[pending.channel] = pending.redelivered
		}
	})
}

func (connection *connectionState) shouldDropClientFrame(value frame) bool {
	if connection.mode != modeConsumerAck || value.kind != 1 || len(value.payload) < 12 {
		return false
	}
	classID, methodID := binary.BigEndian.Uint16(value.payload[0:2]), binary.BigEndian.Uint16(value.payload[2:4])
	if classID != 60 || methodID != 80 {
		return false
	}
	deliveryTag := binary.BigEndian.Uint64(value.payload[4:12])
	connection.mu.Lock()
	target := connection.targetDelivery[value.channel]
	connection.mu.Unlock()
	if target == 0 || target != deliveryTag {
		return false
	}
	connection.mu.Lock()
	redelivered := connection.targetRedelivered[value.channel]
	connection.mu.Unlock()
	return connection.state.observeConsumerAck(redelivered)
}

func (connection *connectionState) shouldDropServerFrame(value frame) bool {
	if connection.mode != modePublisherConfirm || value.kind != 1 || len(value.payload) < 12 {
		return false
	}
	classID, methodID := binary.BigEndian.Uint16(value.payload[0:2]), binary.BigEndian.Uint16(value.payload[2:4])
	if classID != 60 || methodID != 80 {
		return false
	}
	deliveryTag := binary.BigEndian.Uint64(value.payload[4:12])
	connection.mu.Lock()
	eventID := ""
	if bodies := connection.publishBodies[value.channel]; bodies != nil {
		eventID = bodies[deliveryTag]
	}
	connection.mu.Unlock()
	if connection.state.snapshot().TargetEvent == "" {
		if connection.state.waitForTarget(publisherTargetWait) == "" {
			return false
		}
	}
	return connection.state.observePublisherConfirm(eventID)
}

func readFrame(reader io.Reader) (frame, error) {
	header := make([]byte, 7)
	if _, err := io.ReadFull(reader, header); err != nil {
		return frame{}, err
	}
	length := int(binary.BigEndian.Uint32(header[3:7]))
	if length > maxFramePayload {
		return frame{}, errors.New("AMQP frame is too large")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return frame{}, err
	}
	ending := make([]byte, 1)
	if _, err := io.ReadFull(reader, ending); err != nil {
		return frame{}, err
	}
	if ending[0] != frameEnd {
		return frame{}, errors.New("AMQP frame end marker is invalid")
	}
	return frame{kind: header[0], channel: binary.BigEndian.Uint16(header[1:3]), payload: payload}, nil
}

func writeFrame(writer io.Writer, value frame) error {
	if len(value.payload) > maxFramePayload {
		return errors.New("AMQP frame is too large")
	}
	header := make([]byte, 7)
	header[0] = value.kind
	binary.BigEndian.PutUint16(header[1:3], value.channel)
	binary.BigEndian.PutUint32(header[3:7], uint32(len(value.payload)))
	if _, err := writer.Write(header); err != nil {
		return err
	}
	if _, err := writer.Write(value.payload); err != nil {
		return err
	}
	_, err := writer.Write([]byte{frameEnd})
	return err
}

func readShortString(value []byte) (string, int, bool) {
	if len(value) == 0 || int(value[0]) >= len(value) {
		return "", 0, false
	}
	length := int(value[0])
	return string(value[1 : 1+length]), 1 + length, true
}

func eventIDFromBody(body []byte) string {
	var envelope struct {
		EventID string `json:"event_id"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.EventID) < 16 || len(envelope.EventID) > 128 {
		return ""
	}
	return envelope.EventID
}

func appendBounded(existing, value []byte, limit int) []byte {
	if len(existing) >= limit {
		return existing
	}
	remaining := limit - len(existing)
	if len(value) > remaining {
		value = value[:remaining]
	}
	return append(existing, value...)
}
