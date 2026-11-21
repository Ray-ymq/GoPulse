package readcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/logging"
)

type Client interface {
	MGet(context.Context, []string) ([]any, error)
	SetMany(context.Context, map[string]string, time.Time) error
	DeleteMany(context.Context, []string) error
}

type EpochSource interface {
	ReadyEpoch(context.Context) (string, error)
}

type Scope struct {
	client  Client
	epochs  EpochSource
	timeout time.Duration
	slots   chan struct{}
	logger  *slog.Logger
}

const loadTimeout = 2 * time.Second

func NewScope(client Client, epochs EpochSource, timeout time.Duration, maxLoads int, logger *slog.Logger) *Scope {
	if logger == nil {
		logger = logging.Discard("backend")
	}
	return &Scope{client: client, epochs: epochs, timeout: timeout, slots: make(chan struct{}, max(1, maxLoads)), logger: logging.Module(logger, "cache")}
}

type epochKey struct{}

func WithEpoch(ctx context.Context, epoch string) context.Context {
	if epoch == "" {
		return ctx
	}
	return context.WithValue(ctx, epochKey{}, epoch)
}

func (scope *Scope) Epoch(ctx context.Context) (string, error) {
	if epoch, _ := ctx.Value(epochKey{}).(string); epoch != "" {
		return epoch, nil
	}
	return scope.epochs.ReadyEpoch(ctx)
}

type Cache[K comparable, V any] struct {
	scope   *Scope
	kind    string
	ttl     time.Duration
	valid   func(K, V) bool
	suffix  func(K) string
	mu      sync.Mutex
	pending map[string]*load[V]
}

type load[V any] struct {
	done  chan struct{}
	value V
	found bool
	err   error
}

func New[K comparable, V any](scope *Scope, kind string, ttl time.Duration, valid func(K, V) bool) *Cache[K, V] {
	return NewWithKeySuffix(scope, kind, ttl, valid, func(id K) string { return fmt.Sprint(id) })
}

func NewWithKeySuffix[K comparable, V any](scope *Scope, kind string, ttl time.Duration, valid func(K, V) bool, suffix func(K) string) *Cache[K, V] {
	return &Cache[K, V]{scope: scope, kind: kind, ttl: ttl, valid: valid, suffix: suffix, pending: make(map[string]*load[V])}
}

func (cache *Cache[K, V]) Epoch(ctx context.Context) (string, error) {
	return cache.scope.Epoch(ctx)
}

func (cache *Cache[K, V]) key(epoch string, id K) string {
	return "gopulse:" + cache.kind + ":epoch:" + epoch + ":" + cache.suffix(id)
}

func (cache *Cache[K, V]) Fetch(ctx context.Context, epoch string, ids []K, source func(context.Context, []K) (map[K]V, error)) (map[K]V, error) {
	if len(ids) == 0 || epoch == "" {
		return source(ctx, ids)
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = cache.key(epoch, id)
	}
	hits, readErr := cache.get(ctx, keys)
	results := make(map[K]V, len(ids))
	for i, item := range hits {
		value, ok := item.(string)
		id := ids[i]
		var decoded V
		if ok && json.Unmarshal([]byte(value), &decoded) == nil && cache.valid(id, decoded) {
			results[id] = decoded
		}
	}
	if len(results) == len(ids) {
		return results, nil
	}
	owned := make([]K, 0, len(ids)-len(results))
	waiting := make(map[K]*load[V])
	cache.mu.Lock()
	for i, id := range ids {
		if _, ok := results[id]; ok {
			continue
		}
		key := keys[i]
		entry := cache.pending[key]
		if entry == nil {
			entry = &load[V]{done: make(chan struct{})}
			cache.pending[key] = entry
			owned = append(owned, id)
		}
		waiting[id] = entry
	}
	cache.mu.Unlock()

	if len(owned) != 0 {
		cache.fill(ctx, epoch, owned, waiting, source, readErr == nil)
	}
	for id, entry := range waiting {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.done:
			if entry.err != nil {
				return nil, entry.err
			}
			if entry.found {
				results[id] = entry.value
			}
		}
	}
	return results, nil
}

func (cache *Cache[K, V]) FetchOne(ctx context.Context, epoch string, id K, source func(context.Context) (V, bool, error)) (V, bool, error) {
	values, err := cache.Fetch(ctx, epoch, []K{id}, func(ctx context.Context, _ []K) (map[K]V, error) {
		value, found, err := source(ctx)
		if err != nil || !found {
			return nil, err
		}
		return map[K]V{id: value}, nil
	})
	value, found := values[id]
	return value, found, err
}

func (cache *Cache[K, V]) get(ctx context.Context, keys []string) ([]any, error) {
	ctx, cancel := context.WithTimeout(ctx, cache.scope.timeout)
	defer cancel()
	values, err := cache.scope.client.MGet(ctx, keys)
	if err != nil || len(values) != len(keys) {
		return nil, errors.New("读取 Redis 缓存失败")
	}
	return values, nil
}

func (cache *Cache[K, V]) fill(ctx context.Context, epoch string, owned []K, waiting map[K]*load[V], source func(context.Context, []K) (map[K]V, error), writeBack bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loadTimeout)
	defer cancel()
	var values map[K]V
	var err error
	select {
	case cache.scope.slots <- struct{}{}:
		expires := time.Now().Add(cache.ttl)
		values, err = source(ctx, owned)
		<-cache.scope.slots
		if err == nil && writeBack {
			cache.store(ctx, epoch, values, expires)
		}
	case <-ctx.Done():
		err = ctx.Err()
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	for _, id := range owned {
		entry := waiting[id]
		entry.value, entry.found = values[id]
		entry.err = err
		key := cache.key(epoch, id)
		if cache.pending[key] == entry {
			delete(cache.pending, key)
		}
		close(entry.done)
	}
}

func (cache *Cache[K, V]) store(ctx context.Context, epoch string, values map[K]V, expires time.Time) {
	payloads := make(map[string]string, len(values))
	for id, value := range values {
		if cache.valid(id, value) {
			if payload, err := json.Marshal(value); err == nil {
				payloads[cache.key(epoch, id)] = string(payload)
			}
		}
	}
	if len(payloads) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, cache.scope.timeout)
	defer cancel()
	_ = cache.scope.client.SetMany(ctx, payloads, expires)
}

func (cache *Cache[K, V]) Invalidate(ctx context.Context, epoch string, ids ...K) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cache.scope.timeout)
	defer cancel()
	var err error
	if epoch == "" {
		epoch, err = cache.Epoch(ctx)
	}
	if err == nil {
		keys := make([]string, len(ids))
		cache.mu.Lock()
		for i, id := range ids {
			keys[i] = cache.key(epoch, id)
			delete(cache.pending, keys[i])
		}
		cache.mu.Unlock()
		err = cache.scope.client.DeleteMany(ctx, keys)
	}
	if err != nil {
		cache.scope.logger.WarnContext(ctx, "Redis 缓存失效失败", slog.String("cache_type", cache.kind), slog.Any("error", err))
	}
}
