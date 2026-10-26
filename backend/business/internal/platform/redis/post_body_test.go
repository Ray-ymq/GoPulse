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
	get func(context.Context, string) (string, error)
	set func(context.Context, string, string, time.Duration) error
}

func (c *fakeClient) Get(ctx context.Context, k string) (string, error) { return c.get(ctx, k) }
func (c *fakeClient) Set(ctx context.Context, k, v string, ttl time.Duration) error {
	return c.set(ctx, k, v, ttl)
}
func bodyFixture() (post.BodyKey, post.Body) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	return post.BodyKey{Epoch: "epoch-a", PostID: 31, ContentRevision: 4}, post.Body{ID: 31, AuthorID: 7, ContentRevision: 4, Title: "title", Content: "content", CreatedAt: now, UpdatedAt: now}
}
func encodeBody(t *testing.T, version int, body post.Body) string {
	t.Helper()
	payload, err := json.Marshal(postBodyEnvelope{Version: version, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
func TestPostBodyRepositoryRoundTripAndTTL(t *testing.T) {
	key, body := bodyFixture()
	var stored string
	repository := NewPostBodyRepository(&fakeClient{
		get: func(_ context.Context, k string) (string, error) {
			if k != PostBodyKey(key) {
				t.Fatalf("key=%q", k)
			}
			return stored, nil
		},
		set: func(_ context.Context, k, v string, ttl time.Duration) error {
			if k != PostBodyKey(key) || ttl < 270*time.Second || ttl > 330*time.Second {
				t.Fatalf("key=%q ttl=%s", k, ttl)
			}
			stored = v
			return nil
		},
	}, 5*time.Minute, 50*time.Millisecond)
	if err := repository.SetBody(context.Background(), key, body); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"comment_count", "like_count", "username", "bookmarked_by_me", "following", "liked_by_me", "password", "jwt"} {
		if strings.Contains(stored, forbidden) {
			t.Fatalf("cached field %s: %s", forbidden, stored)
		}
	}
	got, hit, err := repository.GetBody(context.Background(), key)
	if err != nil || !hit || got != body {
		t.Fatalf("body=%+v hit=%t error=%v", got, hit, err)
	}
}
func TestPostBodyRepositoryMissAndClientFailure(t *testing.T) {
	key, _ := bodyFixture()
	failure := errors.New("redis unavailable")
	for _, test := range []struct {
		err  error
		want error
	}{{platform.ErrRedisKeyNotFound, nil}, {failure, failure}} {
		repository := NewPostBodyRepository(&fakeClient{get: func(context.Context, string) (string, error) { return "", test.err }}, time.Minute, time.Second)
		_, hit, err := repository.GetBody(context.Background(), key)
		if hit || !errors.Is(err, test.want) {
			t.Fatalf("hit=%t err=%v want=%v", hit, err, test.want)
		}
	}
}
func TestPostBodyRepositoryRejectsDamagedOrMismatchedValues(t *testing.T) {
	key, body := bodyFixture()
	wrongID := body
	wrongID.ID++
	wrongRevision := body
	wrongRevision.ContentRevision++
	missing := body
	missing.Title = ""
	missingAuthor := body
	missingAuthor.AuthorID = 0
	for name, value := range map[string]string{
		"invalid JSON": "{", "retired envelope": `{"version":1,"post":{}}`, "old version": encodeBody(t, 1, body), "missing title": encodeBody(t, 2, missing), "missing author": encodeBody(t, 2, missingAuthor), "wrong id": encodeBody(t, 2, wrongID), "wrong revision": encodeBody(t, 2, wrongRevision), "unknown field": strings.TrimSuffix(encodeBody(t, 2, body), "}") + `,"liked_by_me":true}`, "trailing data": encodeBody(t, 2, body) + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			repository := NewPostBodyRepository(&fakeClient{get: func(context.Context, string) (string, error) { return value, nil }}, time.Minute, time.Second)
			_, hit, err := repository.GetBody(context.Background(), key)
			if hit || !errors.Is(err, ErrInvalidPostBody) {
				t.Fatalf("hit=%t error=%v", hit, err)
			}
		})
	}
}
func TestPostBodyRepositoryRejectsInvalidKeysAndBodiesBeforeIO(t *testing.T) {
	key, body := bodyFixture()
	repository := NewPostBodyRepository(&fakeClient{get: func(context.Context, string) (string, error) { t.Fatal("unexpected GET"); return "", nil }, set: func(context.Context, string, string, time.Duration) error { t.Fatal("unexpected SET"); return nil }}, time.Minute, time.Second)
	for _, invalid := range []post.BodyKey{{}, {Epoch: "bad:epoch", PostID: 31, ContentRevision: 4}, {Epoch: "epoch-a", PostID: 31}} {
		if _, _, err := repository.GetBody(context.Background(), invalid); !errors.Is(err, ErrInvalidPostBody) {
			t.Fatal(err)
		}
		if err := repository.SetBody(context.Background(), invalid, body); !errors.Is(err, ErrInvalidPostBody) {
			t.Fatal(err)
		}
	}
	body.ContentRevision++
	if err := repository.SetBody(context.Background(), key, body); !errors.Is(err, ErrInvalidPostBody) {
		t.Fatal(err)
	}
}
func TestPostBodyRepositoryOperationsUseShortTimeouts(t *testing.T) {
	key, body := bodyFixture()
	repository := NewPostBodyRepository(&fakeClient{get: func(ctx context.Context, _ string) (string, error) { <-ctx.Done(); return "", ctx.Err() }, set: func(ctx context.Context, _, _ string, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }}, time.Minute, 20*time.Millisecond)
	if _, _, err := repository.GetBody(context.Background(), key); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := repository.SetBody(context.Background(), key, body); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
