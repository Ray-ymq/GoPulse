package rediscache

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

type fakeClient struct {
	get    func(context.Context, string) (string, error)
	set    func(context.Context, string, string, time.Duration) error
	delete func(context.Context, string) error
}

func (client *fakeClient) Get(ctx context.Context, key string) (string, error) {
	return client.get(ctx, key)
}

func (client *fakeClient) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return client.set(ctx, key, value, ttl)
}

func (client *fakeClient) Delete(ctx context.Context, key string) error {
	return client.delete(ctx, key)
}

func cachedProjection() post.PublicProjection {
	createdAt := time.Date(2026, 9, 1, 12, 0, 0, 123456000, time.UTC)
	return post.PublicProjection{
		ID:           31,
		Title:        "title",
		Content:      "content",
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
		Author:       post.Author{ID: 7, Username: "author"},
		CommentCount: 2,
		LikeCount:    3,
	}
}

func encodedEnvelope(t *testing.T, version int, projection post.PublicProjection) string {
	t.Helper()
	payload, err := json.Marshal(postDetailEnvelope{Version: version, Post: projection})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestPostDetailRepositoryGetHitAndMiss(t *testing.T) {
	projection := cachedProjection()
	repository := NewPostDetailRepository(&fakeClient{
		get: func(_ context.Context, key string) (string, error) {
			if key != "gopulse:post:detail:v1:31" {
				t.Fatalf("key=%q", key)
			}
			return encodedEnvelope(t, postDetailVersion, projection), nil
		},
	}, 5*time.Minute, 50*time.Millisecond)

	got, hit, err := repository.Get(context.Background(), 31)
	if err != nil || !hit || got != projection {
		t.Fatalf("Get() projection=%#v hit=%t error=%v", got, hit, err)
	}

	miss := NewPostDetailRepository(&fakeClient{get: func(context.Context, string) (string, error) {
		return "", platform.ErrRedisKeyNotFound
	}}, 5*time.Minute, 50*time.Millisecond)
	_, hit, err = miss.Get(context.Background(), 31)
	if err != nil || hit {
		t.Fatalf("miss Get() hit=%t error=%v", hit, err)
	}
}

func TestPostDetailRepositoryRejectsDamagedVersionedOrIncompleteValues(t *testing.T) {
	projection := cachedProjection()
	missingTitle := projection
	missingTitle.Title = ""
	wrongID := projection
	wrongID.ID = 99
	tests := []struct {
		name  string
		value string
	}{
		{name: "invalid json", value: "{"},
		{name: "old version", value: encodedEnvelope(t, 0, projection)},
		{name: "missing required field", value: encodedEnvelope(t, postDetailVersion, missingTitle)},
		{name: "wrong key identity", value: encodedEnvelope(t, postDetailVersion, wrongID)},
		{name: "unknown field", value: strings.TrimSuffix(encodedEnvelope(t, postDetailVersion, projection), "}") + `,"liked_by_me":true}`},
		{name: "trailing data", value: encodedEnvelope(t, postDetailVersion, projection) + `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := NewPostDetailRepository(&fakeClient{get: func(context.Context, string) (string, error) {
				return test.value, nil
			}}, 5*time.Minute, 50*time.Millisecond)
			_, hit, err := repository.Get(context.Background(), 31)
			if hit || !errors.Is(err, ErrInvalidPostDetail) {
				t.Fatalf("Get() hit=%t error=%v", hit, err)
			}
		})
	}
}

func TestPostDetailRepositorySetUsesVersionedPublicJSONAndTTL(t *testing.T) {
	projection := cachedProjection()
	var value string
	repository := NewPostDetailRepository(&fakeClient{set: func(_ context.Context, key, got string, ttl time.Duration) error {
		if key != PostDetailKey(31) || ttl != 5*time.Minute {
			t.Fatalf("Set() key=%q ttl=%s", key, ttl)
		}
		value = got
		return nil
	}}, 5*time.Minute, 50*time.Millisecond)

	if err := repository.Set(context.Background(), projection); err != nil {
		t.Fatalf("Set() error=%v", err)
	}
	if strings.Contains(value, "liked_by_me") || strings.Contains(value, "password") || strings.Contains(value, "jwt") {
		t.Fatalf("cached value contains forbidden field: %s", value)
	}
	var envelope postDetailEnvelope
	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Version != postDetailVersion || envelope.Post != projection {
		t.Fatalf("envelope=%#v", envelope)
	}
}

func TestPostDetailRepositorySetRejectsInvalidProjection(t *testing.T) {
	called := false
	repository := NewPostDetailRepository(&fakeClient{set: func(context.Context, string, string, time.Duration) error {
		called = true
		return nil
	}}, 5*time.Minute, 50*time.Millisecond)
	projection := cachedProjection()
	projection.Author.Username = ""
	if err := repository.Set(context.Background(), projection); !errors.Is(err, ErrInvalidPostDetail) {
		t.Fatalf("Set() error=%v", err)
	}
	if called {
		t.Fatal("client Set called for invalid projection")
	}
}

func TestPostDetailRepositoryInvalidatesExpectedKey(t *testing.T) {
	repository := NewPostDetailRepository(&fakeClient{delete: func(_ context.Context, key string) error {
		if key != PostDetailKey(31) {
			t.Fatalf("Delete() key=%q", key)
		}
		return nil
	}}, 5*time.Minute, 50*time.Millisecond)
	if err := repository.Invalidate(context.Background(), 31); err != nil {
		t.Fatalf("Invalidate() error=%v", err)
	}
}

func TestPostDetailRepositoryOperationsUseShortTimeouts(t *testing.T) {
	client := &fakeClient{
		get: func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
		set: func(ctx context.Context, _, _ string, _ time.Duration) error {
			<-ctx.Done()
			return ctx.Err()
		},
		delete: func(ctx context.Context, _ string) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	repository := NewPostDetailRepository(client, 5*time.Minute, 20*time.Millisecond)
	started := time.Now()
	if _, _, err := repository.Get(context.Background(), 31); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Get() error=%v", err)
	}
	if err := repository.Set(context.Background(), cachedProjection()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Set() error=%v", err)
	}
	if err := repository.Invalidate(context.Background(), 31); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Invalidate() error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("operations took %s, want bounded timeout", elapsed)
	}
}
