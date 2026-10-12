package main

import (
	"net/http"
	"strings"
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

func TestResponseLossOnlyDropsACommittedPostResponse(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/posts", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"meta":{"command_id":"123e4567-e89b-12d3-a456-426614174001","event_id":"123e4567-e89b-12d3-a456-426614174002","post_id":7}}`)
	if !shouldDropCommittedHTTP(modeResponseAfterCommit, request, http.StatusCreated, payload) {
		t.Fatal("committed post response was not selected for loss")
	}
	if shouldDropCommittedHTTP(modeResponseAfterCommit, request, http.StatusConflict, payload) {
		t.Fatal("conflict response was selected for loss")
	}
	request.URL.Path = "/api/v1/post-commands/context"
	if shouldDropCommittedHTTP(modeResponseAfterCommit, request, http.StatusCreated, payload) {
		t.Fatal("non-post response was selected for loss")
	}
}
