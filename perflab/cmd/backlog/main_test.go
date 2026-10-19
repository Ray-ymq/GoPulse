package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubCounter struct {
	pending int64
	err     error
}

func (stub stubCounter) Pending(context.Context) (int64, error) {
	return stub.pending, stub.err
}

func TestHandlerReportsPendingOutbox(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/backlog", nil)
	recorder := httptest.NewRecorder()
	newHandler(stubCounter{pending: 42}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content type = %q, want application/json", got)
	}
	var body map[string]int64
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["pending"] != 42 {
		t.Errorf("pending = %d, want 42", body["pending"])
	}
}

func TestHandlerRejectsNonGet(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/backlog", nil)
	recorder := httptest.NewRecorder()
	newHandler(stubCounter{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if got := recorder.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("allow header = %q, want GET", got)
	}
}

func TestHandlerFailsWhenCounterFails(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/backlog", nil)
	recorder := httptest.NewRecorder()
	newHandler(stubCounter{err: errors.New("database unavailable")}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestValidateLoopbackListen(t *testing.T) {
	for _, address := range []string{"127.0.0.1:18081", "[::1]:18081"} {
		if err := validateLoopbackListen(address); err != nil {
			t.Errorf("loopback address %q rejected: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:18081", ":18081", "192.0.2.10:18081", "not-an-address"} {
		if err := validateLoopbackListen(address); err == nil {
			t.Errorf("non-loopback address %q accepted", address)
		}
	}
}
