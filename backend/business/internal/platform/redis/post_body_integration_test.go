//go:build integration

package rediscache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationPostBodyRedisTTLAndRevisionIsolation(t *testing.T) {
	cfg := integrationtest.Environment(t)
	client := platform.NewRedis(cfg.Redis)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key, body := bodyFixture()
	key.PostID = uint64(time.Now().UnixNano())
	body.ID = key.PostID
	nextKey := key
	nextKey.ContentRevision++
	nextBody := body
	nextBody.ContentRevision++
	nextBody.Content = "new content"
	otherEpoch := key
	otherEpoch.Epoch = "epoch-b"
	for _, k := range []string{PostBodyKey(key), PostBodyKey(nextKey)} {
		defer client.Delete(context.Background(), k)
	}
	repository := NewPostBodyRepository(client, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout)
	if err := repository.SetBody(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	ttl, err := client.TTL(ctx, PostBodyKey(key))
	if err != nil || ttl <= 0 || ttl > cfg.Redis.PostDetailTTL*11/10 {
		t.Fatalf("ttl=%s err=%v", ttl, err)
	}
	if err := repository.SetBody(ctx, nextKey, nextBody); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		keyEpoch bool
		revision bool
	}{{false, false}, {false, true}, {true, false}} {
		k, want := key, body
		if test.revision {
			k, want = nextKey, nextBody
		}
		if test.keyEpoch {
			k = otherEpoch
		}
		got, hit, err := repository.GetBody(ctx, k)
		if err != nil || hit == test.keyEpoch || (!test.keyEpoch && got != want) {
			t.Fatalf("key=%+v body=%+v hit=%t error=%v", k, got, hit, err)
		}
	}
	if err := client.Set(ctx, PostBodyKey(key), `{"version":1,"post":{}}`, cfg.Redis.PostDetailTTL); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := repository.GetBody(ctx, key); hit || !errors.Is(err, ErrInvalidPostBody) {
		t.Fatalf("damaged hit=%t error=%v", hit, err)
	}
}
