package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	searchResultTTL          = 30 * time.Second
	maximumCachedResultBytes = 2048
)

type ResultCacheClient interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, time.Duration) error
}

type resultCache struct {
	client  ResultCacheClient
	timeout time.Duration
}

type cachedResult struct {
	PostIDs []uint64 `json:"post_ids"`
}

func (service *Service) WithResultCache(client ResultCacheClient, operationTimeout time.Duration) *Service {
	service.resultCache = &resultCache{client: client, timeout: operationTimeout}
	return service
}

func resultCacheKey(epoch, generation, queryDigest string, page, offset, limit int) string {
	return fmt.Sprintf("gopulse:search:posts:v1:epoch:%s:index:%s:q:%s:page:%d:offset:%d:limit:%d",
		epoch, generation, queryDigest[:16], page, offset, limit)
}

func (cache *resultCache) get(ctx context.Context, key string, limit int) []uint64 {
	ctx, cancel := context.WithTimeout(ctx, cache.timeout)
	defer cancel()
	value, err := cache.client.Get(ctx, key)
	if err != nil || len(value) > maximumCachedResultBytes {
		return nil
	}
	var result cachedResult
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || len(result.PostIDs) == 0 || len(result.PostIDs) > limit+1 {
		return nil
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil
	}
	seen := make(map[uint64]struct{}, len(result.PostIDs))
	for _, id := range result.PostIDs {
		if _, exists := seen[id]; id == 0 || exists {
			return nil
		}
		seen[id] = struct{}{}
	}
	return result.PostIDs
}

func (cache *resultCache) set(ctx context.Context, key string, ids []uint64) {
	payload, err := json.Marshal(cachedResult{PostIDs: ids})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, cache.timeout)
	defer cancel()
	_ = cache.client.Set(ctx, key, string(payload), searchResultTTL)
}
