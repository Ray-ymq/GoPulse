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

type scriptFakeRedis struct {
	value        any
	sha          string
	loadCalls    int
	evalCalls    int
	evalSHACalls int
	flushOnce    bool
}

func (redis *scriptFakeRedis) Eval(_ context.Context, _ string, _ []string, _ ...any) (any, error) {
	redis.evalCalls++
	return redis.value, nil
}

func (redis *scriptFakeRedis) EvalSha(_ context.Context, _ string, _ []string, _ ...any) (any, error) {
	redis.evalSHACalls++
	if redis.flushOnce {
		redis.flushOnce = false
		return nil, errors.New("NOSCRIPT No matching script. Please use EVAL.")
	}
	return redis.value, nil
}

func (redis *scriptFakeRedis) ScriptLoad(_ context.Context, _ string) (string, error) {
	redis.loadCalls++
	return redis.sha, nil
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
	for _, required := range []string{"redis.call('TIME')", "HMGET", "HSET", "PEXPIRE", "identity_exists"} {
		if !strings.Contains(redis.script, required) {
			t.Fatalf("script does not contain %q", required)
		}
	}
	for _, required := range []string{"tonumber(ARGV[5])", "tonumber(ARGV[6])", "tonumber(ARGV[7])", "tonumber(ARGV[8])"} {
		if !strings.Contains(redis.script, required) {
			t.Fatalf("script does not use argument %q", required)
		}
	}
	if len(redis.args) != 8 || redis.args[0] != float64(2) || redis.args[3] != int64(time.Minute/time.Millisecond) || redis.args[7] != float64(1) {
		t.Fatalf("args=%#v", redis.args)
	}
}

func TestSharedLimiterReusesScriptSHAAndReloadsAfterFlush(t *testing.T) {
	redis := &scriptFakeRedis{value: []any{int64(1), int64(0)}, sha: "test-sha", flushOnce: true}
	limiter := NewSharedLimiter(redis, time.Second)

	for index := 0; index < 3; index++ {
		decision, err := limiter.Allow(context.Background(), testPolicy(), "user:17")
		if err != nil || !decision.Allowed {
			t.Fatalf("Allow(%d) decision=%#v error=%v", index, decision, err)
		}
	}
	if redis.evalCalls != 0 {
		t.Fatalf("Eval() calls=%d, want cached EvalSha path", redis.evalCalls)
	}
	if redis.evalSHACalls != 4 {
		t.Fatalf("EvalSha() calls=%d, want initial call, reload retry, and second cached call", redis.evalSHACalls)
	}
	if redis.loadCalls != 2 {
		t.Fatalf("ScriptLoad() calls=%d, want initial load plus NOSCRIPT reload", redis.loadCalls)
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

type leaseFakeRedis struct {
	value    any
	evalCall int
}

func (redis *leaseFakeRedis) Eval(_ context.Context, _ string, _ []string, _ ...any) (any, error) {
	redis.evalCall++
	return redis.value, nil
}

func TestSharedLimiterAmortizesLeasedReadDecisions(t *testing.T) {
	policy := testPolicy()
	policy.Capacity = 10
	policy.Lease = 4
	redis := &leaseFakeRedis{value: []any{int64(1), int64(0), int64(4)}}
	limiter := NewSharedLimiter(redis, time.Second)
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	limiter.leases.now = func() time.Time { return now }
	for index := 0; index < 4; index++ {
		decision, err := limiter.Allow(context.Background(), policy, "user:17")
		if err != nil || !decision.Allowed {
			t.Fatalf("Allow(%d) decision=%#v error=%v", index, decision, err)
		}
	}
	if redis.evalCall != 1 {
		t.Fatalf("Eval() calls=%d, want one shared decision per four leased requests", redis.evalCall)
	}
	// A partial grant is stored without inventing extra tokens.
	redis.value = []any{int64(1), int64(0), int64(2)}
	for index := 0; index < 3; index++ {
		if _, err := limiter.Allow(context.Background(), policy, "user:18"); err != nil {
			t.Fatal(err)
		}
	}
	if redis.evalCall != 3 {
		t.Fatalf("Eval() calls=%d, want two shared decisions for a two-token grant", redis.evalCall)
	}
	// The expired lease falls back to a new shared decision.
	now = now.Add(2 * time.Minute)
	if _, err := limiter.Allow(context.Background(), policy, "user:17"); err != nil {
		t.Fatal(err)
	}
	if redis.evalCall != 4 {
		t.Fatalf("Eval() calls=%d, want renewal after lease expiry", redis.evalCall)
	}
}

func TestSharedLimiterDoesNotLeaseBelowRequestCost(t *testing.T) {
	policy := testPolicy()
	policy.Lease = 0
	redis := &leaseFakeRedis{value: []any{int64(1), int64(0), int64(1)}}
	limiter := NewSharedLimiter(redis, time.Second)
	for index := 0; index < 3; index++ {
		if _, err := limiter.Allow(context.Background(), policy, "user:17"); err != nil {
			t.Fatal(err)
		}
	}
	if redis.evalCall != 3 {
		t.Fatalf("Eval() calls=%d, want one decision per request without a lease", redis.evalCall)
	}
	if err := (Policy{Name: "post_read", KeyPrefix: "k", Capacity: 2, RefillPerSecond: 1, Cost: 1, IdleTTL: time.Minute,
		CreationCapacity: 1, CreationRate: 1, CreationTTL: time.Minute, Lease: 3}).Validate(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("lease above capacity error=%v", err)
	}
}

func TestMemoryLimiterPreservesQuotasAndExpiresIdentities(t *testing.T) {
	limiter, err := NewMemoryLimiter(2)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	limiter.now = func() time.Time { return now }
	policy := testPolicy()
	policy.CreationCapacity, policy.CreationRate = 1, 1
	policy.Capacity = 1
	allow := func(id string) Decision {
		t.Helper()
		d, err := limiter.Allow(context.Background(), policy, id)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if !allow("user:1").Allowed {
		t.Fatal("首次请求应获准")
	}
	if allow("user:1").Reason != "rate_limited" {
		t.Fatal("身份配额未生效")
	}
	if allow("user:2").Reason != "key_creation_limited" {
		t.Fatal("创建配额未生效")
	}
	now = now.Add(time.Second)
	if !allow("user:2").Allowed {
		t.Fatal("创建配额未恢复")
	}
	if allow("user:3").Reason != "local_capacity" {
		t.Fatal("身份表上限未生效")
	}
	now = now.Add(2 * time.Minute)
	if !allow("user:3").Allowed {
		t.Fatal("过期身份未释放")
	}
	other, _ := NewMemoryLimiter(2)
	if d, err := other.Allow(context.Background(), policy, "user:3"); err != nil || !d.Allowed {
		t.Fatal("实例应独立计数")
	}
}
