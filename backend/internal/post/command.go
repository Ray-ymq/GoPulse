package post

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/tracing"
)

var (
	ErrIdempotencyConflict      = errors.New("post idempotency key conflicts with the original request")
	ErrDataEpochMismatch        = errors.New("post command data epoch does not match the current ready epoch")
	ErrCommandResultUnknown     = errors.New("post command result is unknown")
	ErrCommandOutcomeUnknown    = errors.New("post command outcome is unknown")
	ErrBusinessAsyncBacklog     = errors.New("business async backlog budget is exhausted")
	ErrCommandActivation        = errors.New("post command activation is not ready")
	ErrCommandHeadersIncomplete = errors.New("post command identity headers must be supplied together")
)

// digestCreateInput is versioned and length-delimited so equivalent Unicode
// input has one digest after NormalizeCreateInput, independent of JSON field
// order or the caller's original whitespace.
func digestCreateInput(input CreateInput) []byte {
	var encoded bytes.Buffer
	encoded.WriteByte(byte(businessstate.DigestVersionV1))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(input.Title)))
	encoded.Write(length[:])
	encoded.WriteString(input.Title)
	binary.BigEndian.PutUint32(length[:], uint32(len(input.Content)))
	encoded.Write(length[:])
	encoded.WriteString(input.Content)
	digest := sha256.Sum256(encoded.Bytes())
	return append([]byte(nil), digest[:]...)
}

func validateCommandRequest(request CommandRequest) error {
	if request.DataEpoch == "" && request.IdempotencyKey == "" {
		return nil
	}
	if request.DataEpoch == "" || request.IdempotencyKey == "" {
		return ErrCommandHeadersIncomplete
	}
	if businessstate.ValidateEpoch(request.DataEpoch) != nil || businessstate.ValidateEpoch(request.IdempotencyKey) != nil {
		return fmt.Errorf("%w: command identity is not a canonical UUID", ErrInvalidCommandIdentity)
	}
	return nil
}

var ErrInvalidCommandIdentity = errors.New("post command identity is invalid")

func commandKeyHash(key string) []byte {
	digest := sha256.Sum256([]byte(key))
	return append([]byte(nil), digest[:]...)
}

func commandMeta(commandID, eventID, epoch string, postID uint64, committedAt time.Time, replayed bool, state string) CommandMeta {
	return CommandMeta{CommandID: commandID, EventID: eventID, DataEpoch: epoch, PostID: postID, CommittedAt: committedAt.UTC(), Replayed: replayed, ResourceState: state}
}

func (service *Service) mapCommandError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidCommandIdentity), errors.Is(err, ErrCommandHeadersIncomplete):
		return validationError("post command identity is invalid")
	case errors.Is(err, ErrIdempotencyConflict):
		return apperror.New(apperror.CodeIdempotencyConflict, "the idempotency key was already used with different content")
	case errors.Is(err, ErrDataEpochMismatch):
		return apperror.New(apperror.CodeDataEpochMismatch, "the post command belongs to another data epoch")
	case errors.Is(err, ErrCommandResultUnknown):
		return apperror.New(apperror.CodeCommandResultUnknown, "the post command result is not available")
	case errors.Is(err, ErrCommandOutcomeUnknown):
		return apperror.New(apperror.CodeCommandOutcomeUnknown, "提交结果尚未确认，请使用相同身份查证或重试")
	case errors.Is(err, ErrBusinessAsyncBacklog):
		return apperror.New(apperror.CodeBusinessAsyncBacklog, "business async backlog is temporarily full")
	case errors.Is(err, ErrCommandActivation), errors.Is(err, businessstate.ErrNotReady), errors.Is(err, businessstate.ErrActivationNotDrained):
		return apperror.New(apperror.CodeCommandOutcomeUnknown, "post command activation is not ready")
	default:
		return apperror.WrapInternal(err)
	}
}

func (service *Service) CreateWithCommand(ctx context.Context, authorID uint64, input CreateInput, request CommandRequest) (CreateResult, error) {
	normalized, err := NormalizeCreateInput(input)
	if err != nil {
		if errors.Is(err, ErrInvalidTitle) {
			return CreateResult{}, validationError("title must contain between 1 and 120 characters")
		}
		if errors.Is(err, ErrInvalidContent) {
			return CreateResult{}, validationError("content must contain between 1 and 10000 characters")
		}
		return CreateResult{}, apperror.WrapInternal(err)
	}
	if err := validateCommandRequest(request); err != nil {
		return CreateResult{}, service.mapCommandError(err)
	}
	commandWriter, ok := service.repository.(interface {
		CreateWithCommand(context.Context, uint64, string, string, CommandRequest, []byte) (CreateResult, error)
	})
	if !ok {
		if request.Keyed() {
			return CreateResult{}, service.mapCommandError(ErrCommandActivation)
		}
		record, err := service.Create(ctx, authorID, normalized)
		if err != nil {
			return CreateResult{}, err
		}
		return CreateResult{Post: &record}, nil
	}
	result, err := commandWriter.CreateWithCommand(ctx, authorID, normalized.Title, normalized.Content, request, digestCreateInput(normalized))
	if err != nil {
		// Focused legacy repositories used by the existing synchronous API and
		// integration fixtures intentionally do not own the v2 activation
		// boundary. Preserve the original no-key contract there; keyed calls
		// must never silently fall back to a second non-idempotent write.
		if !request.Keyed() && errors.Is(err, ErrCommandActivation) {
			record, createErr := service.Create(ctx, authorID, normalized)
			if createErr != nil {
				return CreateResult{}, createErr
			}
			return CreateResult{Post: &record}, nil
		}
		return CreateResult{}, service.mapCommandError(err)
	}
	return result, nil
}

func (service *Service) CommandContext(ctx context.Context) (CommandContext, error) {
	reader, ok := service.repository.(interface {
		CommandContext(context.Context) (CommandContext, error)
	})
	if !ok {
		return CommandContext{}, service.mapCommandError(ErrCommandActivation)
	}
	result, err := reader.CommandContext(ctx)
	if err != nil {
		return CommandContext{}, service.mapCommandError(err)
	}
	return result, nil
}

func (service *Service) CommandResult(ctx context.Context, userID uint64, request CommandRequest) (CreateResult, error) {
	if err := validateCommandRequest(request); err != nil || !request.Keyed() {
		if err == nil {
			err = ErrCommandHeadersIncomplete
		}
		return CreateResult{}, service.mapCommandError(err)
	}
	reader, ok := service.repository.(interface {
		CommandResult(context.Context, uint64, CommandRequest) (CreateResult, error)
	})
	if !ok {
		return CreateResult{}, service.mapCommandError(ErrCommandActivation)
	}
	result, err := reader.CommandResult(ctx, userID, request)
	if err != nil {
		return CreateResult{}, service.mapCommandError(err)
	}
	return result, nil
}

func commandReadContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
}

func setCommandTrace(envelope *bus.Envelope, ctx context.Context) {
	if envelope == nil || ctx == nil {
		return
	}
	if wire := tracing.WireFromContext(ctx); wire.TraceParent != "" {
		envelope.SetTraceContext(wire.TraceParent, wire.TraceState)
	}
}
