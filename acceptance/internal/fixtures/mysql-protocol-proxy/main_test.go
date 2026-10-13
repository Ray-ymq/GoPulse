package main

import (
	"bytes"
	"testing"
)

func TestMySQLPacketRoundTrip(t *testing.T) {
	payload := []byte{0x00, 0x01, 0xfe, 0xff}
	var wire bytes.Buffer
	if err := writePacket(&wire, payload, 7); err != nil {
		t.Fatalf("writePacket() error = %v", err)
	}
	got, sequence, err := readPacket(&wire)
	if err != nil {
		t.Fatalf("readPacket() error = %v", err)
	}
	if !bytes.Equal(got, payload) || sequence != 7 {
		t.Fatalf("packet = %#v/%d, want %#v/7", got, sequence, payload)
	}
}

func TestSuccessfulMySQLResponsesExcludeErrors(t *testing.T) {
	for _, test := range []struct {
		payload []byte
		want    bool
	}{
		{payload: []byte{0x00}, want: true},
		{payload: []byte{0xfe}, want: true},
		{payload: []byte{0xff}, want: false},
		{payload: nil, want: false},
	} {
		if got := isSuccessfulResponse(test.payload); got != test.want {
			t.Fatalf("isSuccessfulResponse(%#v) = %t, want %t", test.payload, got, test.want)
		}
	}
}
