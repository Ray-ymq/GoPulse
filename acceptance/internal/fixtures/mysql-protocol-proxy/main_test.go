package main

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTargetRequiresExplicitArm(t *testing.T) {
	store := &statusStore{status: status{Mode: modeCommitResult}}
	if store.claimTarget() {
		t.Fatal("target was claimed before arm")
	}
	store.arm()
	if !store.claimTarget() {
		t.Fatal("armed target was not claimed")
	}
	if store.claimTarget() {
		t.Fatal("target remained armed after one claim")
	}
	if got := store.snapshot().TargetQueries; got != 1 {
		t.Fatalf("target queries = %d, want 1", got)
	}
}

func TestStatusHandlerArmsWithPOSTOnly(t *testing.T) {
	store := &statusStore{status: status{Mode: modeCommitResult}}
	get := httptest.NewRecorder()
	statusHandler(store).ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/arm", nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /arm status = %d, want %d", get.Code, http.StatusMethodNotAllowed)
	}
	post := httptest.NewRecorder()
	statusHandler(store).ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/arm", nil))
	if post.Code != http.StatusNoContent {
		t.Fatalf("POST /arm status = %d, want %d", post.Code, http.StatusNoContent)
	}
	if !store.claimTarget() {
		t.Fatal("POST /arm did not arm the next target")
	}
}

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

func TestPreparedStatementTargetIsRegisteredFromPrepareResponse(t *testing.T) {
	state := &connectionState{}
	state.notePrepare(targetQuery(modeCompletionWrite, "INSERT INTO business_application_results (event_id) VALUES (?)"))
	response := make([]byte, 5)
	response[0] = 0x00
	binary.LittleEndian.PutUint32(response[1:], 42)
	state.registerPreparedResponse(response)
	if !state.isPreparedTarget(42) {
		t.Fatal("prepared completion statement was not registered")
	}
	if state.isPreparedTarget(43) {
		t.Fatal("unrelated prepared statement was registered")
	}
}

func TestTargetQueryRecognizesTextAndPreparedModes(t *testing.T) {
	if !targetQuery(modeCommitResult, "COMMIT") {
		t.Fatal("COMMIT was not recognized")
	}
	if !targetQuery(modeCompletionWrite, "INSERT INTO BUSINESS_APPLICATION_RESULTS VALUES (?)") {
		t.Fatal("completion INSERT was not recognized")
	}
	if targetQuery(modeCompletionWrite, "SELECT EXISTS(SELECT 1 FROM BUSINESS_APPLICATION_RESULTS)") {
		t.Fatal("completion lookup was incorrectly recognized")
	}
	if targetQuery(modeCompletionWrite, "SELECT * FROM posts") {
		t.Fatal("unrelated query was recognized")
	}
}
