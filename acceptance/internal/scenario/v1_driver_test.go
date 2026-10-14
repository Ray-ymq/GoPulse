package scenario

import (
	"strings"
	"testing"
	"time"
)

func TestPrivateComponentMetricsBypassesHostProxy(t *testing.T) {
	transport := privateMetricsTransport()
	defer transport.CloseIdleConnections()
	if transport.Proxy != nil {
		t.Fatal("private component metrics probe must bypass the host proxy")
	}
}

func TestHasPreciseLogRequiresTheRequestedIdentity(t *testing.T) {
	meta := v1HTTPMeta{CommandID: "123e4567-e89b-42d3-a456-426614174001", DataEpoch: "123e4567-e89b-42d3-a456-426614174000"}
	var stage v1StageResult
	stage.Evidence.Logs.Quality = "known"
	stage.Evidence.Logs.Entries = append(stage.Evidence.Logs.Entries, struct {
		CommandID string `json:"command_id"`
		DataEpoch string `json:"data_epoch"`
	}{CommandID: "123e4567-e89b-42d3-a456-426614174099", DataEpoch: meta.DataEpoch})
	if hasPreciseLog(stage, meta) {
		t.Fatal("a log for another command was accepted")
	}
	stage.Evidence.Logs.Entries = append(stage.Evidence.Logs.Entries, struct {
		CommandID string `json:"command_id"`
		DataEpoch string `json:"data_epoch"`
	}{CommandID: meta.CommandID, DataEpoch: meta.DataEpoch})
	if !hasPreciseLog(stage, meta) {
		t.Fatal("the matching log was not accepted")
	}
	stage.Evidence.Logs.Quality = "no_evidence"
	if hasPreciseLog(stage, meta) {
		t.Fatal("no-evidence logs were accepted as precise evidence")
	}
}

func TestFormalCaseDoesNotInventIdentity(t *testing.T) {
	runner := &formalV1Runner{commandEpoch: "123e4567-e89b-42d3-a456-426614174000", rawDirectory: t.TempDir(), deadline: time.Now().Add(time.Minute)}
	if item := runner.completedCase("V02", v1HTTPMeta{}, time.Now(), "unused", false, false, nil, 0, 0); item.Status != "not_started" || item.CaseID != "V02" {
		t.Fatalf("empty completed case was materialized: %+v", item)
	}
	if item := runner.failedCase("V03", identityFromMeta(v1HTTPMeta{}), time.Now(), "no identity", false, nil, 0, 0); item.Status != "not_started" || item.CaseID != "V03" {
		t.Fatalf("empty failed case was materialized: %+v", item)
	}
}

func TestDurableFactsQueryReadsEpochFromOutboxPayload(t *testing.T) {
	query := durableFactsQuery(v1HTTPMeta{DataEpoch: "123e4567-e89b-42d3-a456-426614174000", CommandID: "123e4567-e89b-42d3-a456-426614174001", EventID: "123e4567-e89b-42d3-a456-426614174002", PostID: 7})
	if !strings.Contains(query, "JSON_UNQUOTE(JSON_EXTRACT(payload,'$.data_epoch'))") {
		t.Fatal("outbox epoch predicate does not read the envelope payload")
	}
	if strings.Contains(query, "business_outbox WHERE data_epoch") {
		t.Fatal("outbox query refers to a non-existent data_epoch column")
	}
}
