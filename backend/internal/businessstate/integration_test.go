//go:build integration

package businessstate_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationBusinessIdentityEvidenceAndParking(t *testing.T) {
	cfg := integrationtest.Environment(t)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repository, err := businessstate.NewRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := repository.Ensure(ctx)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	baseline, err := repository.Budget(ctx, environment.DataEpoch)
	if err != nil {
		t.Fatalf("Budget() baseline error = %v", err)
	}
	cleanupEventID := ""
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if cleanupEventID != "" {
			_, _ = database.ExecContext(cleanupCtx, `DELETE FROM business_application_results WHERE data_epoch=? AND event_id=?`, environment.DataEpoch, cleanupEventID)
			_, _ = database.ExecContext(cleanupCtx, `DELETE FROM business_event_requirements WHERE data_epoch=? AND event_id=?`, environment.DataEpoch, cleanupEventID)
			_, _ = database.ExecContext(cleanupCtx, `DELETE FROM business_event_failures WHERE data_epoch=? AND event_id=?`, environment.DataEpoch, cleanupEventID)
			_, _ = database.ExecContext(cleanupCtx, `DELETE FROM business_message_parking WHERE event_id=?`, cleanupEventID)
		}
		_, _ = database.ExecContext(cleanupCtx, `UPDATE business_async_budget SET open_requirements=?, retained_groups=?, retained_bytes=?, parked_messages=?, parked_bytes=? WHERE data_epoch=?`, baseline.OpenRequirements, baseline.RetainedGroups, baseline.RetainedBytes, baseline.ParkedMessages, baseline.ParkedBytes, environment.DataEpoch)
	})

	event, err := bus.NewV2PostCreated(time.Now().UTC(), environment.DataEpoch, "123e4567-e89b-12d3-a456-426614174001", 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	cleanupEventID = event.EventID
	payloadBytes := 128
	if err := repository.RegisterRequirement(ctx, businessstate.Requirement{
		DataEpoch: environment.DataEpoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch,
		RoutingContractRevision: businessstate.SearchRoutingContractV1, ApplicableObjectVersion: 1, PayloadBytes: payloadBytes,
	}); err != nil {
		t.Fatalf("RegisterRequirement() error = %v", err)
	}
	payload := []byte(fmt.Sprintf("phase24-parking-%s", event.EventID))
	oldEpoch := "123e4567-e89b-12d3-a456-426614174002"
	if err := repository.ParkMessage(ctx, businessstate.ParkingMessage{
		SourceEpoch: oldEpoch, EventID: event.EventID, EventType: string(bus.PostCreated), SchemaVersion: bus.SchemaVersionV2,
		RoutingKey: bus.PostCreatedRoutingKey, SourceKind: "integration", Payload: payload, Reason: "integration_epoch",
	}); err != nil {
		t.Fatalf("ParkMessage() error = %v", err)
	}
	if err := repository.RecordFailure(ctx, businessstate.Failure{DataEpoch: environment.DataEpoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch, Kind: "retry", Reason: "temporary_backend", OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatalf("RecordFailure() error = %v", err)
	}
	if err := repository.RecordApplication(ctx, businessstate.Application{
		DataEpoch: environment.DataEpoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch,
		RoutingContractRevision: businessstate.SearchRoutingContractV1, ProjectionGeneration: "gopulse-post-search-v1-integration",
		Result: "applied", ActualVersion: 1, CompletedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordApplication() error = %v", err)
	}
	if found, err := repository.FindApplication(ctx, businessstate.Application{
		DataEpoch: environment.DataEpoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch,
		RoutingContractRevision: businessstate.SearchRoutingContractV1, ProjectionGeneration: "gopulse-post-search-v1-integration", Result: "applied",
	}); err != nil || !found {
		t.Fatalf("FindApplication() found=%v error=%v", found, err)
	}
	if budget, err := repository.Budget(ctx, environment.DataEpoch); err != nil {
		t.Fatal(err)
	} else if budget.OpenRequirements != baseline.OpenRequirements || budget.RetainedGroups != baseline.RetainedGroups+1 || budget.RetainedBytes != baseline.RetainedBytes+uint64(payloadBytes+businessstate.RetentionMetadataBytes) || budget.ParkedMessages != baseline.ParkedMessages+1 || budget.ParkedBytes != baseline.ParkedBytes+uint64(len(payload)) {
		t.Fatalf("budget=%#v baseline=%#v", budget, baseline)
	}

	accepted, err := repository.CheckEnvelopeForConsumer(ctx, event, businessstate.ConsumerSearch)
	if err != nil || accepted != businessstate.AdmissionAccept {
		t.Fatalf("current v2 admission=%v err=%v", accepted, err)
	}
	oldEvent, err := bus.NewV2PostLiked(time.Now().UTC(), oldEpoch, 1, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	parked, err := repository.CheckEnvelope(ctx, oldEvent)
	if err != nil || parked != businessstate.AdmissionPark {
		t.Fatalf("old v2 admission=%v err=%v", parked, err)
	}

}
