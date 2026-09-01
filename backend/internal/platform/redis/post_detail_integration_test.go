//go:build integration

package rediscache

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

func TestIntegrationPostDetailRedisTTLContentInvalidationAndRebuild(t *testing.T) {
	cfg := integrationtest.Environment(t)
	client := platform.NewRedis(cfg.Redis)
	defer client.Close()

	postID := uint64(time.Now().UnixNano())
	key := PostDetailKey(postID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer client.Delete(context.Background(), key)

	projection := post.PublicProjection{
		ID:           postID,
		Title:        "integration title",
		Content:      "integration content",
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		UpdatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		Author:       post.Author{ID: 7, Username: "integration-author"},
		CommentCount: 2,
		LikeCount:    3,
	}
	repository := NewPostDetailRepository(client, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout)
	if err := repository.Set(ctx, projection); err != nil {
		t.Fatalf("Set() error=%v", err)
	}

	ttl, err := client.TTL(ctx, key)
	if err != nil || ttl <= 0 || ttl > cfg.Redis.PostDetailTTL {
		t.Fatalf("TTL()=%s error=%v, configured=%s", ttl, err, cfg.Redis.PostDetailTTL)
	}
	raw, err := client.Get(ctx, key)
	if err != nil {
		t.Fatalf("raw Get() error=%v", err)
	}
	for _, forbidden := range []string{"liked_by_me", "password", "jwt", "cookie"} {
		if strings.Contains(strings.ToLower(raw), forbidden) {
			t.Fatalf("cached value contains %q: %s", forbidden, raw)
		}
	}
	got, hit, err := repository.Get(ctx, postID)
	if err != nil || !hit || got != projection {
		t.Fatalf("Get() projection=%#v hit=%t error=%v", got, hit, err)
	}

	if err := repository.Invalidate(ctx, postID); err != nil {
		t.Fatalf("Invalidate() error=%v", err)
	}
	_, hit, err = repository.Get(ctx, postID)
	if err != nil || hit {
		t.Fatalf("Get() after invalidation hit=%t error=%v", hit, err)
	}

	projection.CommentCount = 4
	if err := repository.Set(ctx, projection); err != nil {
		t.Fatalf("rebuild Set() error=%v", err)
	}
	got, hit, err = repository.Get(ctx, postID)
	if err != nil || !hit || got.CommentCount != 4 {
		t.Fatalf("rebuilt Get() projection=%#v hit=%t error=%v", got, hit, err)
	}

	if err := client.Set(ctx, key, `{"version":0}`, cfg.Redis.PostDetailTTL); err != nil {
		t.Fatalf("write damaged value: %v", err)
	}
	_, hit, err = repository.Get(ctx, postID)
	if hit || err == nil {
		t.Fatalf("damaged Get() hit=%t error=%v", hit, err)
	}
}
