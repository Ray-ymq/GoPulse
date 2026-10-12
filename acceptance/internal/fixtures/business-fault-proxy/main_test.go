package main

import (
	"net/http"
	"testing"
	"time"
)

func TestFaultRulesOnlyDropTheirDeclaredBoundary(t *testing.T) {
	if !shouldDrop(modeResponseAfterCommit, http.Header{"X-GoPulse-Commit": []string{"durable"}}) {
		t.Fatal("durable commit response was not dropped")
	}
	if shouldDrop(modeResponseAfterCommit, http.Header{"X-GoPulse-Broker-Confirmation": []string{"durable"}}) {
		t.Fatal("broker confirmation was confused with commit response")
	}
	if !shouldDrop(modeBrokerConfirm, http.Header{"X-GoPulse-Broker-Confirmation": []string{"durable"}}) {
		t.Fatal("durable broker confirmation was not dropped")
	}
	if err := validateConfig("127.0.0.1:19124", "http://127.0.0.1:8080", modeDelay, 61*time.Second); err == nil {
		t.Fatal("unbounded delay was accepted")
	}
}
