package postcache

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform/readcache"
)

type bodyBatchFixture struct {
	readKeys []string
	values   map[string]string
}

func (fixture *bodyBatchFixture) MGet(_ context.Context, keys []string) ([]any, error) {
	fixture.readKeys = keys
	hits := make([]any, len(keys))
	for i, key := range keys {
		hits[i] = fixture.values[key]
	}
	return hits, nil
}

func (fixture *bodyBatchFixture) SetMany(_ context.Context, values map[string]string, _ time.Time) error {
	for key, value := range values {
		fixture.values[key] = value
	}
	return nil
}

func (fixture *bodyBatchFixture) DeleteMany(context.Context, []string) error {
	return nil
}

func TestFetchBodiesReusesTheDetailKeyAndEnvelope(t *testing.T) {
	key, body := bodyFixture()
	second := key
	second.PostID++
	missing := body
	missing.ID = second.PostID

	fixture := &bodyBatchFixture{values: map[string]string{PostBodyKey(key): encodeBody(t, 2, body)}}
	repository := NewPostBodyRepository(&fakeClient{get: func(_ context.Context, key string) (string, error) {
		return fixture.values[key], nil
	}}, time.Minute, time.Second)
	repository.WithBatch(readcache.NewScope(fixture, nil, time.Second, 1, nil))
	keys := []post.BodyKey{key, second}
	bodies, err := repository.FetchBodies(context.Background(), key.Epoch, keys, func(_ context.Context, keys []post.BodyKey) (map[post.BodyKey]post.Body, error) {
		if !slices.Equal(keys, []post.BodyKey{second}) {
			t.Fatalf("回源的正文键为 %v，预期仅回源第二个键", keys)
		}
		return map[post.BodyKey]post.Body{second: missing}, nil
	})
	if err != nil || len(bodies) != 2 || bodies[key] != body || bodies[second] != missing ||
		!slices.Equal(fixture.readKeys, []string{PostBodyKey(key), PostBodyKey(second)}) {
		t.Fatalf("批量读取结果为 %v，键为 %v，错误为 %v", bodies, fixture.readKeys, err)
	}
	if loaded, hit, err := repository.GetBody(context.Background(), second); err != nil || !hit || loaded != missing {
		t.Fatalf("详情读取批量回填的正文为 %v，命中为 %t，错误为 %v", loaded, hit, err)
	}
}
