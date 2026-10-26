package post

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

func TestCreateDigestUsesNormalizedVersionedFields(t *testing.T) {
	first, err := NormalizeCreateInput(CreateInput{Title: "  标题\u3000", Content: "\n正文\t"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizeCreateInput(CreateInput{Title: "标题", Content: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	if string(digestCreateInput(first)) != string(digestCreateInput(second)) {
		t.Fatal("equivalent normalized input produced different digests")
	}
	if string(digestCreateInput(first)) == string(digestCreateInput(CreateInput{Title: "标题正文", Content: ""})) {
		t.Fatal("digest encoding is not field-delimited")
	}
}

func TestValidateCommandRequestRequiresCanonicalPairedIdentities(t *testing.T) {
	validEpoch := "123e4567-e89b-12d3-a456-426614174000"
	for _, test := range []struct {
		name    string
		request CommandRequest
		wantErr error
	}{
		{name: "unkeyed", request: CommandRequest{}},
		{name: "missing epoch", request: CommandRequest{IdempotencyKey: validEpoch}, wantErr: ErrCommandHeadersIncomplete},
		{name: "missing key", request: CommandRequest{DataEpoch: validEpoch}, wantErr: ErrCommandHeadersIncomplete},
		{name: "non uuid", request: CommandRequest{DataEpoch: validEpoch, IdempotencyKey: "key"}, wantErr: ErrInvalidCommandIdentity},
		{name: "uppercase uuid", request: CommandRequest{DataEpoch: validEpoch, IdempotencyKey: "123E4567-E89B-12D3-A456-426614174000"}, wantErr: ErrInvalidCommandIdentity},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateCommandRequest(test.request)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("validateCommandRequest() error = %v", err)
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("validateCommandRequest() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

type commandRepository struct {
	*fakeRepository
	createWithCommand func(context.Context, uint64, string, string, CommandRequest, []byte) (CreateResult, error)
}

func (repository *commandRepository) CreateWithCommand(ctx context.Context, userID uint64, title, content string, request CommandRequest, digest []byte) (CreateResult, error) {
	return repository.createWithCommand(ctx, userID, title, content, request, digest)
}

func TestServiceCreateWithCommandNormalizesAndMapsDurableErrors(t *testing.T) {
	called := false
	repository := &commandRepository{
		fakeRepository: &fakeRepository{},
		createWithCommand: func(_ context.Context, userID uint64, title, content string, request CommandRequest, digest []byte) (CreateResult, error) {
			called = true
			if userID != 17 || title != "标题" || content != "正文" || !request.Keyed() || len(digest) != 32 {
				t.Fatalf("command arguments user=%d title=%q content=%q request=%#v digest=%d", userID, title, content, request, len(digest))
			}
			return CreateResult{}, ErrIdempotencyConflict
		},
	}
	service := NewService(repository)
	_, err := service.CreateWithCommand(context.Background(), 17, CreateInput{Title: "  标题 ", Content: "\n正文\t"}, CommandRequest{
		DataEpoch:      "123e4567-e89b-12d3-a456-426614174000",
		IdempotencyKey: "123e4567-e89b-12d3-a456-426614174001",
	})
	if !called {
		t.Fatal("command writer was not called")
	}
	appError, ok := apperror.As(err)
	if !ok || appError.Code != apperror.CodeIdempotencyConflict {
		t.Fatalf("error=%#v, want idempotency conflict", err)
	}
}

func TestCommandMetaUsesUTCStableCommitTime(t *testing.T) {
	committed := time.Date(2026, 9, 2, 3, 4, 5, 123456000, time.FixedZone("test", 8*60*60))
	meta := commandMeta(
		"123e4567-e89b-12d3-a456-426614174001",
		"123e4567-e89b-12d3-a456-426614174002",
		"123e4567-e89b-12d3-a456-426614174000",
		7,
		committed,
		true,
		"available",
	)
	if meta.CommittedAt.Location() != time.UTC || !meta.CommittedAt.Equal(committed) || !meta.Replayed || meta.ResourceState != "available" {
		t.Fatalf("meta=%#v", meta)
	}
}
