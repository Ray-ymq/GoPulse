package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrUnavailable means that the shared limiter could not produce a decision.
// Callers must choose the endpoint-specific failure policy instead of retrying
// an unknown Redis script result.
var ErrUnavailable = errors.New("shared rate limiter unavailable")

// ErrInvalidPolicy is returned during assembly or configuration validation.
var ErrInvalidPolicy = errors.New("invalid rate limit policy")

// RedisClient is deliberately narrower than a go-redis client. The limiter
// only needs one atomic script entry point, which also keeps tests independent
// of a Redis server.
type RedisClient interface {
	Eval(context.Context, string, []string, ...any) (any, error)
}

type scriptRedisClient interface {
	RedisClient
	EvalSha(context.Context, string, []string, ...any) (any, error)
	ScriptLoad(context.Context, string) (string, error)
}

// Policy describes one stable quota. Names and prefixes are configuration
// vocabulary; request data must only be supplied as the bounded identity.
type Policy struct {
	Name             string
	KeyPrefix        string
	Capacity         float64
	RefillPerSecond  float64
	Cost             float64
	IdleTTL          time.Duration
	CreationCapacity float64
	CreationRate     float64
	CreationTTL      time.Duration
	// Lease is the largest number of tokens one shared decision may reserve for
	// this process. Zero or one keeps an exact shared decision per request.
	// A lease never grants tokens Redis did not deduct; unused leased tokens
	// are lost, so the aggregate quota stays an upper bound. Only high-volume
	// policies should opt in.
	Lease float64
}

func (policy Policy) leaseSize() float64 {
	if policy.Lease < policy.Cost {
		return policy.Cost
	}
	return policy.Lease
}

func (policy Policy) Validate() error {
	if !validName(policy.Name) || policy.Capacity <= 0 || policy.RefillPerSecond <= 0 || policy.Cost <= 0 || policy.Cost > policy.Capacity {
		return fmt.Errorf("%w: quota values for %q are invalid", ErrInvalidPolicy, policy.Name)
	}
	if policy.Lease < 0 || policy.Lease > policy.Capacity || policy.Lease > 0 && policy.Lease < policy.Cost {
		return fmt.Errorf("%w: lease for %q is invalid", ErrInvalidPolicy, policy.Name)
	}
	if policy.IdleTTL <= 0 || policy.IdleTTL > 365*24*time.Hour {
		return fmt.Errorf("%w: idle TTL for %q is invalid", ErrInvalidPolicy, policy.Name)
	}
	if policy.CreationCapacity <= 0 || policy.CreationRate <= 0 || policy.CreationTTL <= 0 {
		return fmt.Errorf("%w: key creation budget for %q is invalid", ErrInvalidPolicy, policy.Name)
	}
	if policy.KeyPrefix == "" {
		return fmt.Errorf("%w: key prefix for %q is empty", ErrInvalidPolicy, policy.Name)
	}
	return nil
}

func validName(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character != '_' && character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// Decision is the stable result returned by both shared and local limiters.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
	Reason     string
	Mode       string
}

// Limiter is the boundary consumed by HTTP middleware.
type Limiter interface {
	Allow(context.Context, Policy, string) (Decision, error)
}

const tokenBucketScript = `
local function read_state(key)
  local values = redis.call('HMGET', key, 'tokens', 'last_refill_ms')
  return tonumber(values[1]), tonumber(values[2]), values[1] ~= false or values[2] ~= false
end

local function take(key, capacity, refill, cost, ttl, now, tokens, last, lease)
  if tokens == nil then tokens = capacity end
  if last == nil then last = now end
  local elapsed = now - last
  if elapsed < 0 then elapsed = 0 end
  tokens = math.min(capacity, tokens + elapsed * refill / 1000)
  local granted = 0
  if tokens >= cost then
    granted = math.min(lease, math.floor(tokens / cost) * cost)
    if granted < cost then granted = 0 end
  end
  if granted < cost then
    redis.call('HSET', key, 'tokens', tostring(tokens), 'last_refill_ms', tostring(now))
    redis.call('PEXPIRE', key, tostring(ttl))
    return {0, math.ceil((cost - tokens) * 1000 / refill), 0}
  end
  tokens = tokens - granted
  redis.call('HSET', key, 'tokens', tostring(tokens), 'last_refill_ms', tostring(now))
  redis.call('PEXPIRE', key, tostring(ttl))
  return {1, 0, granted}
end

local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local identity_tokens, identity_last, identity_exists = read_state(KEYS[1])
if not identity_exists then
  local creation_tokens, creation_last = read_state(KEYS[2])
  local creation = take(KEYS[2], tonumber(ARGV[5]), tonumber(ARGV[6]), 1, tonumber(ARGV[7]), now, creation_tokens, creation_last, 1)
  if creation[1] ~= 1 then
    return {-2, creation[2], 0}
  end
end
local result = take(KEYS[1], tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4]), now, identity_tokens, identity_last, tonumber(ARGV[8]))
return {result[1], result[2], result[3]}
`

// SharedLimiter executes one atomic Hash + Lua decision. The script obtains
// Redis TIME itself, so independent Backend processes do not use wall-clock
// values from their own machines. Policies with a Lease above the request cost
// reserve a small token block per decision, which is what keeps a high-volume
// read path from paying an extra Redis round trip for every request.
type SharedLimiter struct {
	client       RedisClient
	timeout      time.Duration
	scriptLoadMu sync.Mutex
	scriptSHA    atomic.Pointer[string]
	leases       *leaseCache
}

func NewSharedLimiter(client RedisClient, timeout time.Duration) *SharedLimiter {
	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}
	return &SharedLimiter{client: client, timeout: timeout, leases: newLeaseCache(defaultLeaseEntries)}
}

func (limiter *SharedLimiter) Allow(ctx context.Context, policy Policy, identity string) (Decision, error) {
	if limiter == nil || limiter.client == nil {
		return Decision{}, ErrUnavailable
	}
	if err := policy.Validate(); err != nil {
		return Decision{}, err
	}
	identity = strings.TrimSpace(identity)
	if identity == "" || len(identity) > 128 || strings.ContainsAny(identity, "\r\n\t ") {
		return Decision{}, fmt.Errorf("%w: identity is invalid", ErrInvalidPolicy)
	}
	if limiter.leases.take(policy, identity) {
		// The token was deducted from the shared bucket by an earlier decision.
		return Decision{Allowed: true, Reason: "allow", Mode: "shared"}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	operationContext, cancel := context.WithTimeout(ctx, limiter.timeout)
	defer cancel()
	identityKey := policy.KeyPrefix + identity
	creationKey := "gopulse:rl:v1:creation:" + policy.Name
	keys := []string{identityKey, creationKey}
	args := []any{
		policy.Capacity,
		policy.RefillPerSecond,
		policy.Cost,
		policy.IdleTTL.Milliseconds(),
		policy.CreationCapacity,
		policy.CreationRate,
		policy.CreationTTL.Milliseconds(),
		policy.leaseSize(),
	}
	result, err := limiter.execute(operationContext, keys, args)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: execute shared quota: %v", ErrUnavailable, err)
	}
	code, retry, granted, err := parseResult(result)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: decode shared quota: %v", ErrUnavailable, err)
	}
	decision := Decision{Mode: "shared"}
	if code == 1 {
		limiter.leases.add(policy, identity, granted)
		decision.Allowed = true
		decision.Reason = "allow"
		return decision, nil
	}
	decision.Reason = "rate_limited"
	if code == -2 {
		decision.Reason = "key_creation_limited"
	}
	decision.RetryAfter = time.Duration(retry) * time.Millisecond
	if decision.RetryAfter < time.Millisecond {
		decision.RetryAfter = time.Millisecond
	}
	return decision, nil
}

// execute uses the Redis script cache on clients that expose it. The script is
// loaded lazily because limiter construction must not perform I/O, and the
// SHA is invalidated and rebuilt if an operator flushes Redis scripts while a
// Backend is running. Clients without script-cache support keep the narrow
// Eval-only contract used by unit tests and alternate adapters.
func (limiter *SharedLimiter) execute(ctx context.Context, keys []string, args []any) (any, error) {
	scriptClient, ok := limiter.client.(scriptRedisClient)
	if !ok {
		return limiter.client.Eval(ctx, tokenBucketScript, keys, args...)
	}

	sha, err := limiter.ensureScript(ctx, scriptClient)
	if err != nil {
		return nil, err
	}
	result, err := scriptClient.EvalSha(ctx, sha, keys, args...)
	if err == nil || !isNoScript(err) {
		return result, err
	}

	limiter.invalidateScript(sha)
	sha, err = limiter.ensureScript(ctx, scriptClient)
	if err != nil {
		return nil, err
	}
	return scriptClient.EvalSha(ctx, sha, keys, args...)
}

func (limiter *SharedLimiter) ensureScript(ctx context.Context, client scriptRedisClient) (string, error) {
	if cached := limiter.scriptSHA.Load(); cached != nil {
		return *cached, nil
	}
	limiter.scriptLoadMu.Lock()
	defer limiter.scriptLoadMu.Unlock()
	if cached := limiter.scriptSHA.Load(); cached != nil {
		return *cached, nil
	}
	sha, err := client.ScriptLoad(ctx, tokenBucketScript)
	if err != nil {
		return "", err
	}
	limiter.scriptSHA.Store(&sha)
	return sha, nil
}

func (limiter *SharedLimiter) invalidateScript(sha string) {
	if cached := limiter.scriptSHA.Load(); cached != nil && *cached == sha {
		limiter.scriptSHA.CompareAndSwap(cached, nil)
	}
}

func isNoScript(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "NOSCRIPT")
}

func parseResult(value any) (int64, int64, int64, error) {
	values, ok := value.([]any)
	if !ok || len(values) < 2 || len(values) > 3 {
		return 0, 0, 0, errors.New("unexpected script result")
	}
	code, err := integerResult(values[0])
	if err != nil {
		return 0, 0, 0, err
	}
	retry, err := integerResult(values[1])
	if err != nil {
		return 0, 0, 0, err
	}
	if len(values) == 2 {
		return code, retry, 0, nil
	}
	granted, err := integerResult(values[2])
	if err != nil {
		return 0, 0, 0, err
	}
	return code, retry, granted, nil
}

func integerResult(value any) (int64, error) {
	switch number := value.(type) {
	case int:
		return int64(number), nil
	case int64:
		return number, nil
	case uint64:
		if number > math.MaxInt64 {
			return 0, errors.New("integer result overflows")
		}
		return int64(number), nil
	case float64:
		if number != math.Trunc(number) || number < math.MinInt64 || number > math.MaxInt64 {
			return 0, errors.New("integer result is invalid")
		}
		return int64(number), nil
	default:
		return 0, fmt.Errorf("integer result has type %T", value)
	}
}

type localState struct {
	tokens float64
	last   time.Time
	seen   time.Time
	ttl    time.Duration
}

// LocalLimiter is a deliberately bounded fallback. It is an approximation of
// the shared policy and never grows beyond MaxIdentities; it is not presented
// as a replacement for a distributed quota.
type LocalLimiter struct {
	mu             sync.Mutex
	states         map[string]localState
	maxIdentities  int
	now            func() time.Time
	creationLimits bool
	creations      map[string]localState
	nextSweep      time.Time
}

func NewLocalLimiter(maxIdentities int) (*LocalLimiter, error) {
	if maxIdentities < 1 {
		return nil, errors.New("local limiter capacity must be positive")
	}
	return &LocalLimiter{states: make(map[string]localState, maxIdentities), maxIdentities: maxIdentities, now: time.Now}, nil
}

// NewMemoryLimiter 在单个进程中执行身份配额和新身份创建配额。各实例独立计数。
func NewMemoryLimiter(maxIdentities int) (*LocalLimiter, error) {
	limiter, err := NewLocalLimiter(maxIdentities)
	if err != nil {
		return nil, err
	}
	limiter.creationLimits = true
	limiter.creations = make(map[string]localState)
	return limiter, nil
}

func (limiter *LocalLimiter) Allow(_ context.Context, policy Policy, identity string) (Decision, error) {
	if limiter == nil {
		return Decision{}, ErrUnavailable
	}
	if err := policy.Validate(); err != nil {
		return Decision{}, err
	}
	identity = strings.TrimSpace(identity)
	if identity == "" || len(identity) > 128 || strings.ContainsAny(identity, "\r\n\t ") {
		return Decision{}, fmt.Errorf("%w: identity is invalid", ErrInvalidPolicy)
	}
	now := limiter.now().UTC()
	key := policy.Name + "\x00" + identity
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if !now.Before(limiter.nextSweep) {
		for stateKey, state := range limiter.states {
			if state.ttl > 0 && now.Sub(state.seen) >= state.ttl {
				delete(limiter.states, stateKey)
			}
		}
		for name, state := range limiter.creations {
			if state.ttl > 0 && now.Sub(state.seen) >= state.ttl {
				delete(limiter.creations, name)
			}
		}
		limiter.nextSweep = now.Add(time.Second)
	}
	state, exists := limiter.states[key]
	if !exists {
		if len(limiter.states) >= limiter.maxIdentities {
			return Decision{Mode: "local", Reason: "local_capacity"}, nil
		}
		if limiter.creationLimits {
			creation, found := limiter.creations[policy.Name]
			if !found {
				creation = localState{tokens: policy.CreationCapacity, last: now, ttl: policy.CreationTTL}
			}
			creation.tokens = math.Min(policy.CreationCapacity, creation.tokens+math.Max(0, now.Sub(creation.last).Seconds())*policy.CreationRate)
			creation.last, creation.seen = now, now
			if creation.tokens < 1 {
				limiter.creations[policy.Name] = creation
				return Decision{Mode: "local", Reason: "key_creation_limited", RetryAfter: time.Duration(math.Ceil((1 - creation.tokens) / policy.CreationRate * float64(time.Second)))}, nil
			}
			creation.tokens--
			limiter.creations[policy.Name] = creation
		}
		state = localState{tokens: policy.Capacity, last: now, ttl: policy.IdleTTL}
	}
	elapsed := now.Sub(state.last).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	state.tokens = math.Min(policy.Capacity, state.tokens+elapsed*policy.RefillPerSecond)
	state.last = now
	state.seen = now
	if state.tokens < policy.Cost {
		missing := policy.Cost - state.tokens
		retry := time.Duration(math.Ceil(missing / policy.RefillPerSecond * float64(time.Second)))
		if retry < time.Millisecond {
			retry = time.Millisecond
		}
		limiter.states[key] = state
		return Decision{Mode: "local", Reason: "rate_limited", RetryAfter: retry}, nil
	}
	state.tokens -= policy.Cost
	limiter.states[key] = state
	return Decision{Allowed: true, Mode: "local", Reason: "allow"}, nil
}
