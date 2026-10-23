package ratelimit

import (
	"sync"
	"time"
)

// defaultLeaseEntries bounds the per-process identity lease table. The bound is
// about memory, not quota: when it is reached, an arbitrary entry is dropped
// and its unused tokens are lost, which can only make a decision stricter.
const defaultLeaseEntries = 4096

type leaseEntry struct {
	tokens float64
	seen   time.Time
	ttl    time.Duration
}

// leaseCache keeps tokens that one shared Redis decision already deducted for
// this process. A lease is therefore never a source of extra allowance: the
// aggregate quota remains an upper bound, and an outage loses unused tokens
// instead of exceeding the quota. Tokens are consumed locally one request at a
// time, so the high-volume read path pays one shared round trip per lease
// rather than per request.
type leaseCache struct {
	mu      sync.Mutex
	entries map[string]leaseEntry
	max     int
	now     func() time.Time
}

func newLeaseCache(max int) *leaseCache {
	if max <= 0 {
		max = 1
	}
	return &leaseCache{entries: make(map[string]leaseEntry, max), max: max, now: time.Now}
}

func leaseKey(policy Policy, identity string) string {
	return policy.Name + "\x00" + identity
}

// take consumes one request cost from a live lease. Policies that do not lease
// (Lease at or below Cost) always fall through to the shared decision.
func (cache *leaseCache) take(policy Policy, identity string) bool {
	if cache == nil || policy.Lease <= policy.Cost {
		return false
	}
	now := cache.now()
	key := leaseKey(policy, identity)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[key]
	if !ok {
		return false
	}
	if entry.ttl > 0 && now.Sub(entry.seen) >= entry.ttl {
		delete(cache.entries, key)
		return false
	}
	if entry.tokens < policy.Cost {
		return false
	}
	entry.tokens -= policy.Cost
	entry.seen = now
	cache.entries[key] = entry
	return true
}

// add stores the tokens the current decision reserved beyond the request it
// just authorized.
func (cache *leaseCache) add(policy Policy, identity string, granted int64) {
	if cache == nil || policy.Lease <= policy.Cost || float64(granted) <= policy.Cost {
		return
	}
	now := cache.now()
	key := leaseKey(policy, identity)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[key]
	if !ok {
		if len(cache.entries) >= cache.max {
			for candidate := range cache.entries {
				delete(cache.entries, candidate)
				break
			}
		}
		entry = leaseEntry{ttl: policy.IdleTTL}
	}
	entry.tokens += float64(granted) - policy.Cost
	entry.seen = now
	if entry.ttl <= 0 {
		entry.ttl = policy.IdleTTL
	}
	cache.entries[key] = entry
}
