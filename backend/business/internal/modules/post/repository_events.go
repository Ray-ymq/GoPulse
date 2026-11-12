package post

import (
	"context"
	"database/sql"

	"github.com/Ray-ymq/GoPulse/backend/internal/async/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/modules/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/modules/businessstate"
)

// mutationEpoch reads the activation boundary on the fact transaction.
func (repository *MySQLRepository) mutationEpoch(ctx context.Context, tx *sql.Tx) (string, error) {
	if repository.state == nil {
		return "", nil
	}
	environment, err := repository.state.LockCurrentTx(ctx, tx)
	if err != nil {
		return "", err
	}
	if environment.Status != businessstate.StatusReady || environment.Mode != businessstate.ModeV2 {
		return "", businessstate.ErrNotReady
	}
	return environment.DataEpoch, nil
}

func (repository *MySQLRepository) insertSearchMutation(ctx context.Context, tx *sql.Tx, event bus.Envelope, epoch string) (uint64, error) {
	var requirement businessstate.Requirement
	if epoch != "" {
		var err error
		event, err = event.WithDataEpoch(epoch)
		if err != nil {
			return 0, err
		}

		payload, err := bus.Encode(event)
		if err != nil {
			return 0, err
		}
		requirement = businessstate.Requirement{
			DataEpoch: epoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch,
			RoutingContractRevision: businessstate.SearchRoutingContractV1,
			ApplicableObjectVersion: event.ContentRevision, PayloadBytes: len(payload),
		}
	}
	outboxID, err := insertOutbox(ctx, tx, repository.outbox, event)
	if err != nil {
		return 0, err
	}
	// 先写 Outbox 和需求，再取得预算行锁，使后续持锁区间只覆盖预算检查、扣账和提交。
	if epoch != "" {
		if err := repository.state.RegisterRequirementTx(ctx, tx, requirement); err != nil {
			return 0, err
		}
	}
	return outboxID, nil
}

func insertOutbox(ctx context.Context, executor outbox.Executor, writer outbox.Writer, envelope bus.Envelope) (uint64, error) {
	if metadataWriter, ok := writer.(outbox.MetadataWriter); ok {
		return metadataWriter.InsertWithMetadata(ctx, executor, envelope)
	}
	return 0, writer.Insert(ctx, executor, envelope)
}
