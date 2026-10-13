package main

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestAMQPFrameRoundTrip(t *testing.T) {
	want := frame{kind: 1, channel: 3, payload: []byte{0x00, 0x3c, 0x00, 0x50, 0x01}}
	var wire bytes.Buffer
	if err := writeFrame(&wire, want); err != nil {
		t.Fatalf("writeFrame() error = %v", err)
	}
	got, err := readFrame(&wire)
	if err != nil {
		t.Fatalf("readFrame() error = %v", err)
	}
	if got.kind != want.kind || got.channel != want.channel || !bytes.Equal(got.payload, want.payload) {
		t.Fatalf("frame = %#v, want %#v", got, want)
	}
}

func TestPublisherConfirmDropIsBoundToAnObservedEvent(t *testing.T) {
	state := &store{value: status{Mode: modePublisherConfirm}}
	connection := &connectionState{mode: modePublisherConfirm, state: state}
	connection.init()
	channel := uint16(1)
	publish := frame{kind: 1, channel: channel, payload: []byte{0x00, 0x3c, 0x00, 0x28}}
	connection.inspectClientMethod(publish)
	eventID := "123e4567-e89b-42d3-a456-426614174000"
	body := []byte(`{"event_id":"` + eventID + `"}`)
	headerPayload := make([]byte, 14)
	binary.BigEndian.PutUint16(headerPayload[0:2], 60)
	binary.BigEndian.PutUint16(headerPayload[2:4], 0)
	binary.BigEndian.PutUint64(headerPayload[4:12], uint64(len(body)))
	connection.inspectClientHeader(frame{kind: 2, channel: channel, payload: headerPayload})
	connection.inspectClientBody(frame{kind: 3, channel: channel, payload: body})
	confirm := make([]byte, 13)
	binary.BigEndian.PutUint16(confirm[0:2], 60)
	binary.BigEndian.PutUint16(confirm[2:4], 80)
	binary.BigEndian.PutUint64(confirm[4:12], 1)
	dropped := make(chan bool, 1)
	go func() {
		dropped <- connection.shouldDropServerFrame(frame{kind: 1, channel: channel, payload: confirm})
	}()
	select {
	case <-dropped:
		t.Fatal("publisher confirm was selected before the target was armed")
	case <-time.After(25 * time.Millisecond):
	}
	if err := state.setTarget(eventID); err != nil {
		t.Fatalf("setTarget() error = %v", err)
	}
	select {
	case result := <-dropped:
		if !result {
			t.Fatal("selected publisher confirm was forwarded")
		}
	case <-time.After(time.Second):
		t.Fatal("publisher confirm did not resume after target was armed")
	}
	snapshot := state.snapshot()
	if snapshot.PublisherEventReceived != 1 || snapshot.PublisherConfirmDropped != 1 || snapshot.LastEvent != eventID {
		t.Fatalf("publisher status = %#v", snapshot)
	}
}

func TestConsumerAckDropThenRedeliveryAckIsForwarded(t *testing.T) {
	state := &store{value: status{Mode: modeConsumerAck}}
	connection := &connectionState{mode: modeConsumerAck, state: state}
	connection.init()
	connection.targetDelivery[1] = 7
	ack := make([]byte, 13)
	binary.BigEndian.PutUint16(ack[0:2], 60)
	binary.BigEndian.PutUint16(ack[2:4], 80)
	binary.BigEndian.PutUint64(ack[4:12], 7)
	if !connection.shouldDropClientFrame(frame{kind: 1, channel: 1, payload: ack}) {
		t.Fatal("initial consumer ack was forwarded")
	}
	connection.targetDelivery[1] = 8
	connection.targetRedelivered[1] = true
	binary.BigEndian.PutUint64(ack[4:12], 8)
	if connection.shouldDropClientFrame(frame{kind: 1, channel: 1, payload: ack}) {
		t.Fatal("redelivery consumer ack was dropped")
	}
	snapshot := state.snapshot()
	if snapshot.ConsumerAckDropped != 1 || snapshot.ConsumerRedeliveryAck != 1 || snapshot.Disconnects != 0 {
		t.Fatalf("consumer ack status = %#v", snapshot)
	}
}

func TestEventIDFromBodyRejectsUnboundedOrMalformedBodies(t *testing.T) {
	if got := eventIDFromBody([]byte(`{"event_id":"123e4567-e89b-42d3-a456-426614174000"}`)); got == "" {
		t.Fatal("valid event ID was rejected")
	}
	if got := eventIDFromBody([]byte(`{"event_id":"short"}`)); got != "" {
		t.Fatalf("malformed event ID = %q", got)
	}
}
