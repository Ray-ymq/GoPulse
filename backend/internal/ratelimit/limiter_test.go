package ratelimit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRedis struct {
	script string
	keys   []string
	args   []any
	value  any
	err    error
}

func (redis *fakeRedis) Eval(_ context.Context, script string, keys []string, args ...any) (any, error) {
	redis.script, redis.keys, redis.args = script, keys, args
	return redis.value, redis.err
}

func testPolicy() Policy {
	return Policy{
		Name: "post_read", KeyPrefix: "gopulse:rl:v1:post_read:", Capacity: 2, RefillPerSecond: 1, Cost: 1,
		IdleTTL: time.Minute, CreationCapacity: 10, CreationRate: 1, CreationTTL: time.Minute,
	}
}

func TestSharedLimiterUsesAtomicRedisTimeAndCreationBudget(t *testing.T) {
	redis := &fakeRedis{value: []any{int64(1), int64(0)}}
	limiter := NewSharedLimiter(redis, 50*time.Millisecond)
	decision, err := limiter.Allow(context.Background(), testPolicy(), "user:17")
	if err != nil || !decision.Allowed || decision.Mode != "shared" {
		t.Fatalf("Allow() decision=%#v error=%v", decision, err)
	}
	if len(redis.keys) != 2 || redis.keys[0] != "gopulse:rl:v1:post_read:user:17" || redis.keys[1] != "gopulse:rl:v1:creation:post_read" {
		t.Fatalf("keys=%#v", redis.keys)
	}
	for _, required := range []string{"redis.call('TIME')", "HSET", "PEXPIRE", "identity_exists"} {
		if !strings.Contains(redis.script, required) {
			t.Fatalf("script does not contain %q", required)
		}
	}
	if len(redis.args) != 7 || redis.args[0] != float64(2) || redis.args[3] != int64(time.Minute/time.Millisecond) {
		t.Fatalf("args=%#v", redis.args)
	}
}

func TestSharedLimiterSeparatesRateLimitedAndCreationLimited(t *testing.T) {
	for _, result := range []struct {
		name   string
		value  any
		reason string
	}{
		{name: "identity", value: []any{int64(0), int64(250)}, reason: "rate_limited"},
		{name: "creation", value: []any{int64(-2), int64(700)}, reason: "key_creation_limited"},
	} {
		t.Run(result.name, func(t *testing.T) {
			redis := &fakeRedis{value: result.value}
			decision, err := NewSharedLimiter(redis, time.Second).Allow(context.Background(), testPolicy(), "ip:127.0.0.1")
			if err != nil || decision.Allowed || decision.Reason != result.reason || decision.RetryAfter != time.Duration(result.value.([]any)[1].(int64))*time.Millisecond {
				t.Fatalf("decision=%#v error=%v", decision, err)
			}
		})
	}
}

func TestSharedLimiterRejectsUnknownScriptResultAndUnavailableClient(t *testing.T) {
	if _, err := NewSharedLimiter(nil, time.Second).Allow(context.Background(), testPolicy(), "user:1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil client error=%v", err)
	}
	redis := &fakeRedis{value: []any{int64(1)}, err: nil}
	if _, err := NewSharedLimiter(redis, time.Second).Allow(context.Background(), testPolicy(), "user:1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("invalid script result error=%v", err)
	}
}

func TestLocalLimiterIsBoundedAndRefills(t *testing.T) {
	limiter, err := NewLocalLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	policy := testPolicy()
	first, err := limiter.Allow(context.Background(), policy, "user:1")
	if err != nil || !first.Allowed {
		t.Fatalf("first=%#v error=%v", first, err)
	}
	second, err := limiter.Allow(context.Background(), policy, "user:2")
	if err != nil || second.Reason != "local_capacity" {
		t.Fatalf("second=%#v error=%v", second, err)
	}
	now = now.Add(time.Second)
	third, err := limiter.Allow(context.Background(), policy, "user:1")
	if err != nil || !third.Allowed {
		t.Fatalf("third=%#v error=%v", third, err)
	}
}
