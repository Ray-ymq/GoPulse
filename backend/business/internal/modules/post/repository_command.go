package post

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/modules/businessstate"
)

// CreateWithCommand writes a post fact, command result, v2 post.created event,
// and its search obligation in one MySQL transaction.
// 事务期间不调用 Redis、消息代理或 Elasticsearch。
func (repository *MySQLRepository) CreateWithCommand(ctx context.Context, authorID uint64, title, content string, request CommandRequest, requestDigest []byte) (CreateResult, error) {
	if repository == nil || repository.database == nil || repository.outbox == nil || repository.state == nil {
		return CreateResult{}, ErrCommandActivation
	}
	if ctx == nil || authorID == 0 || len(requestDigest) != 32 {
		return CreateResult{}, errors.New("create post command: invalid arguments")
	}
	if err := validateCommandRequest(request); err != nil {
		return CreateResult{}, err
	}
	starter, ok := repository.database.(transactionStarter)
	if !ok {
		return CreateResult{}, errors.New("create post command: database does not support transactions")
	}
	transaction, err := starter.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return CreateResult{}, errors.New("begin post command transaction")
	}
	defer func() { _ = transaction.Rollback() }()
	environment, err := repository.state.LockCurrentTx(ctx, transaction)
	if err != nil {
		return CreateResult{}, err
	}
	if environment.Status != businessstate.StatusReady || environment.Mode != businessstate.ModeV2 {
		return CreateResult{}, ErrCommandActivation
	}
	now := repository.clock().UTC().Truncate(time.Microsecond)
	keyHash := []byte(nil)
	if request.IdempotencyKey != "" {
		if request.DataEpoch != environment.DataEpoch {
			return CreateResult{}, ErrDataEpochMismatch
		}
		keyHash = commandKeyHash(request.IdempotencyKey)
		existing, found, lookupErr := repository.state.FindCommandTx(ctx, transaction, environment.DataEpoch, authorID, keyHash, now)
		if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
			return CreateResult{}, lookupErr
		}
		if found {
			if !existing.RetainedUntil.IsZero() && !existing.RetainedUntil.After(now) {
				return CreateResult{}, ErrCommandResultUnknown
			}
			if !bytes.Equal(existing.RequestDigest, requestDigest) {
				return CreateResult{}, ErrIdempotencyConflict
			}
			if existing.Status != "succeeded" {
				return CreateResult{}, ErrCommandOutcomeUnknown
			}
			if err := transaction.Commit(); err != nil {
				return CreateResult{}, ErrCommandOutcomeUnknown
			}
			readContext, cancel := commandReadContext(ctx)
			defer cancel()
			return repository.replayResult(readContext, authorID, existing, true, nil), nil
		}
	}
	commandID, err := bus.NewUUID()
	if err != nil {
		return CreateResult{}, errors.New("generate post command ID")
	}
	record, err := createPost(ctx, transaction, authorID, title, content)
	if err != nil {
		return CreateResult{}, err
	}
	event, err := bus.NewV2PostCreated(now, environment.DataEpoch, commandID, authorID, record.ID, record.ContentRevision)
	if err != nil {
		return CreateResult{}, errors.New("create v2 post event")
	}

	payload, err := bus.Encode(event)
	if err != nil {
		return CreateResult{}, errors.New("encode v2 post event")
	}
	reservation := businessstate.CommandReservation{
		Command:       businessstate.Command{DataEpoch: environment.DataEpoch, CommandID: commandID, UserID: authorID, CommandType: businessstate.PostCreateCommandType, IdempotencyKeyHash: keyHash, RequestDigest: requestDigest, PostID: record.ID, EventID: event.EventID, Status: "succeeded"},
		RequestDigest: requestDigest, IdempotencyKeyHash: keyHash,
		Requirement:  businessstate.Requirement{DataEpoch: environment.DataEpoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch, RoutingContractRevision: businessstate.SearchRoutingContractV1, ApplicableObjectVersion: record.ContentRevision, PayloadBytes: len(payload)},
		PayloadBytes: len(payload), Now: now,
	}
	if err := repository.state.ReservePostCreateTx(ctx, transaction, reservation); err != nil {
		if errors.Is(err, businessstate.ErrCommandAlreadyExists) && request.IdempotencyKey != "" {
			// The database uniqueness constraint is the final race boundary. A
			// competing transaction may win after the initial lookup but before
			// this transaction inserts its command. Roll back the losing post
			// fact, then resolve the committed winner through a fresh connection.
			_ = transaction.Rollback()
			lookupContext, cancel := commandReadContext(ctx)
			defer cancel()
			committed, found, lookupErr := repository.state.FindCommand(lookupContext, environment.DataEpoch, authorID, keyHash, now)
			if lookupErr != nil || !found {
				return CreateResult{}, ErrCommandOutcomeUnknown
			}
			if !committed.RetainedUntil.IsZero() && !committed.RetainedUntil.After(now) {
				return CreateResult{}, ErrCommandResultUnknown
			}
			if !bytes.Equal(committed.RequestDigest, requestDigest) {
				return CreateResult{}, ErrIdempotencyConflict
			}
			if committed.Status != "succeeded" {
				return CreateResult{}, ErrCommandOutcomeUnknown
			}
			return repository.replayResult(lookupContext, authorID, committed, true, nil), nil
		}
		if errors.Is(err, businessstate.ErrBudgetExceeded) {
			return CreateResult{}, ErrBusinessAsyncBacklog
		}
		return CreateResult{}, err
	}
	if _, err := insertOutbox(ctx, transaction, repository.outbox, event); err != nil {
		return CreateResult{}, errors.New("create v2 post outbox event")
	}
	if err := repository.state.AccountPostCreateTx(ctx, transaction, reservation); err != nil {
		if errors.Is(err, businessstate.ErrBudgetExceeded) {
			return CreateResult{}, ErrBusinessAsyncBacklog
		}
		return CreateResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		if request.IdempotencyKey == "" {
			return CreateResult{}, ErrCommandOutcomeUnknown
		}
		lookupContext, cancel := commandReadContext(ctx)
		defer cancel()
		committed, found, lookupErr := repository.state.FindCommand(lookupContext, environment.DataEpoch, authorID, keyHash, now)
		if lookupErr == nil && found && committed.Status == "succeeded" && bytes.Equal(committed.RequestDigest, requestDigest) {
			return repository.replayResult(lookupContext, authorID, committed, true, nil), nil
		}
		return CreateResult{}, ErrCommandOutcomeUnknown
	}
	return CreateResult{Post: &record, Meta: commandMeta(commandID, event.EventID, environment.DataEpoch, record.ID, now, false, "available")}, nil
}

func (repository *MySQLRepository) replayResult(ctx context.Context, userID uint64, command businessstate.Command, replayed bool, executor database) CreateResult {
	var record *Post
	resourceState := "unavailable"
	if command.PostID > 0 {
		// The command row is already committed when a replay reaches this
		// path. Read through the repository so viewer-specific permissions and
		// bookmark/like state are recalculated instead of returning a cached
		// author-only projection. The executor is retained in the signature for
		// the first replay transaction's lock scope; it is not used for a
		// second connection's viewer read.
		value, err := repository.FindByID(ctx, command.PostID, userID)
		if err == nil {
			if hydrator, ok := any(repository).(interface {
				HydrateFollowing(context.Context, []Post, uint64) error
			}); ok {
				records := []Post{value}
				if hydrateErr := hydrator.HydrateFollowing(ctx, records, userID); hydrateErr == nil {
					value = records[0]
				}
			}
			record = &value
			resourceState = "available"
		}
	}
	_ = executor
	return CreateResult{Post: record, Meta: commandMeta(command.CommandID, command.EventID, command.DataEpoch, command.PostID, command.CommittedAt, replayed, resourceState)}
}

func (repository *MySQLRepository) CommandContext(ctx context.Context) (CommandContext, error) {
	if repository == nil || repository.state == nil {
		return CommandContext{}, ErrCommandActivation
	}
	value, err := repository.state.ReadCommandContext(ctx)
	if err != nil {
		return CommandContext{}, err
	}
	return CommandContext{DataEpoch: value.DataEpoch, RetryWindow: int64(value.RetryWindow / time.Second), RetryWindowUntil: value.RetryWindowUntil}, nil
}

func (repository *MySQLRepository) CommandResult(ctx context.Context, userID uint64, request CommandRequest) (CreateResult, error) {
	if repository == nil || repository.state == nil {
		return CreateResult{}, ErrCommandActivation
	}
	if err := validateCommandRequest(request); err != nil {
		return CreateResult{}, err
	}
	readContext, cancel := commandReadContext(ctx)
	defer cancel()
	environment, err := repository.state.Current(readContext)
	if err != nil {
		return CreateResult{}, err
	}
	if environment.Status != businessstate.StatusReady || request.DataEpoch != environment.DataEpoch {
		return CreateResult{}, ErrDataEpochMismatch
	}
	keyHash := commandKeyHash(request.IdempotencyKey)
	command, found, err := repository.state.FindCommand(readContext, environment.DataEpoch, userID, keyHash, repository.clock().UTC())
	if err != nil {
		return CreateResult{}, ErrCommandOutcomeUnknown
	}
	if !found || command.Status != "succeeded" {
		return CreateResult{}, ErrCommandResultUnknown
	}
	return repository.replayResult(readContext, userID, command, true, nil), nil
}
