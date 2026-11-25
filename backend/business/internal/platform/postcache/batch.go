package postcache

import (
	"context"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform/readcache"
)

func (repository *PostBodyRepository) WithBatch(scope *readcache.Scope) {
	repository.batch = readcache.NewWithKeySuffix(scope, postBodyCacheKind, repository.ttl, func(key post.BodyKey, value postBodyEnvelope) bool {
		return value.Version == postBodyVersion && value.Body.ValidForKey(key)
	}, func(key post.BodyKey) string {
		return postBodySuffix(key.PostID, key.ContentRevision)
	})
}

func (repository *PostBodyRepository) FetchBodies(ctx context.Context, epoch string, keys []post.BodyKey, load func(context.Context, []post.BodyKey) (map[post.BodyKey]post.Body, error)) (map[post.BodyKey]post.Body, error) {
	for _, key := range keys {
		if !validBodyKey(key) || key.Epoch != epoch {
			return nil, ErrInvalidPostBody
		}
	}
	cached, err := repository.batch.Fetch(ctx, epoch, keys, func(ctx context.Context, misses []post.BodyKey) (map[post.BodyKey]postBodyEnvelope, error) {
		bodies, err := load(ctx, misses)
		if err != nil {
			return nil, err
		}
		values := make(map[post.BodyKey]postBodyEnvelope, len(bodies))
		for key, body := range bodies {
			values[key] = postBodyEnvelope{Version: postBodyVersion, Body: body}
		}
		return values, nil
	})
	if err != nil {
		return nil, err
	}
	bodies := make(map[post.BodyKey]post.Body, len(cached))
	for key, value := range cached {
		bodies[key] = value.Body
	}
	return bodies, nil
}
