package platform

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ErrRedisUnavailable is returned without touching Redis while the local
// recovery gate is cooling down or another caller owns the probe slot.
var ErrRedisUnavailable = errors.New("redis unavailable")

const defaultRedisRecoveryCooldown = time.Second

// redisRecoveryGate keeps a failed Redis dependency from becoming a per
// request retry loop. Normal traffic is concurrent while healthy. After a
// dependency failure, callers fail fast until the cooldown expires; exactly
// one caller may then probe the dependency.
type redisRecoveryGate struct {
	mu           sync.Mutex
	blockedUntil time.Time
	probing      bool
	cooldown     time.Duration
	now          func() time.Time
}

func newRedisRecoveryGate(cooldown time.Duration) *redisRecoveryGate {
	if cooldown <= 0 {
		cooldown = defaultRedisRecoveryCooldown
	}
	return &redisRecoveryGate{cooldown: cooldown, now: time.Now}
}

func (gate *redisRecoveryGate) call(ctx context.Context, operation func() error) error {
	if operation == nil {
		return ErrRedisUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	probe, allowed := gate.begin()
	if !allowed {
		return ErrRedisUnavailable
	}
	err := operation()
	gate.finish(probe, err, ctx)
	return err
}

func (gate *redisRecoveryGate) begin() (probe bool, allowed bool) {
	now := gate.now()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.blockedUntil.IsZero() {
		return false, true
	}
	if gate.probing || now.Before(gate.blockedUntil) {
		return false, false
	}
	gate.probing = true
	return true, true
}

func (gate *redisRecoveryGate) finish(probe bool, err error, ctx context.Context) {
	now := gate.now()
	dependencyFailure := redisDependencyFailure(err, ctx)

	gate.mu.Lock()
	defer gate.mu.Unlock()
	if dependencyFailure {
		gate.blockedUntil = now.Add(gate.cooldown)
	} else if probe {
		// A cancelled probe did not establish recovery. Keep the gate closed
		// for one more bounded interval so a cancellation burst cannot turn
		// into an immediate probe storm.
		if err != nil && ctx.Err() != nil {
			gate.blockedUntil = now.Add(gate.cooldown)
		} else {
			gate.blockedUntil = time.Time{}
		}
	}
	if probe {
		gate.probing = false
	}
}

func redisDependencyFailure(err error, ctx context.Context) bool {
	if err == nil || errors.Is(err, goredis.Nil) || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) && ctx != nil && ctx.Err() != nil {
		return false
	}
	message := strings.ToUpper(err.Error())
	for _, expected := range []string{
		"NOSCRIPT",
		"WRONGTYPE",
		"ERR UNKNOWN COMMAND",
		"ERR SYNTAX",
		"ERR WRONG NUMBER OF ARGUMENTS",
	} {
		if strings.Contains(message, expected) {
			return false
		}
	}
	return true
}
