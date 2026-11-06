package platform

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func TestRedisRecoveryGateSkipsCallsAndAllowsOneProbeAfterCooldown(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	gate := newRedisRecoveryGate(5 * time.Second)
	gate.now = func() time.Time { return now }
	failure := errors.New("dial tcp: connection refused")
	var calls atomic.Int32

	if err := gate.call(context.Background(), func() error {
		calls.Add(1)
		return failure
	}); !errors.Is(err, failure) {
		t.Fatalf("initial call error=%v, want=%v", err, failure)
	}
	if err := gate.call(context.Background(), func() error {
		calls.Add(1)
		return nil
	}); !errors.Is(err, ErrRedisUnavailable) {
		t.Fatalf("cooldown call error=%v, want=%v", err, ErrRedisUnavailable)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls during cooldown=%d, want 1", got)
	}

	now = now.Add(5 * time.Second)
	if err := gate.call(context.Background(), func() error {
		calls.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("recovery probe error=%v", err)
	}
	if err := gate.call(context.Background(), func() error {
		calls.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("healthy call error=%v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls after recovery=%d, want 3", got)
	}
}

func TestRedisRecoveryGateAllowsOnlyOneConcurrentRecoveryProbe(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	gate := newRedisRecoveryGate(time.Second)
	gate.now = func() time.Time { return now }
	if err := gate.call(context.Background(), func() error { return errors.New("redis down") }); err == nil {
		t.Fatal("initial failed call error=nil")
	}
	now = now.Add(time.Second)

	started := make(chan struct{})
	release := make(chan struct{})
	probeResult := make(chan error, 1)
	go func() {
		probeResult <- gate.call(context.Background(), func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	const followers = 31
	followerResults := make(chan error, followers)
	for index := 0; index < followers; index++ {
		go func() {
			followerResults <- gate.call(context.Background(), func() error {
				return errors.New("follower touched Redis")
			})
		}()
	}
	for index := 0; index < followers; index++ {
		if err := <-followerResults; !errors.Is(err, ErrRedisUnavailable) {
			t.Fatalf("follower %d error=%v, want gate rejection", index, err)
		}
	}
	close(release)
	if err := <-probeResult; err != nil {
		t.Fatalf("probe error=%v", err)
	}
}

func TestRedisRecoveryGateDoesNotTripForExpectedRedisResults(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "missing key", err: goredis.Nil},
		{name: "missing script", err: errors.New("NOSCRIPT No matching script")},
		{name: "caller cancelled", err: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			gate := newRedisRecoveryGate(time.Minute)
			if err := gate.call(context.Background(), func() error { return test.err }); !errors.Is(err, test.err) {
				t.Fatalf("first error=%v, want=%v", err, test.err)
			}
			called := false
			if err := gate.call(context.Background(), func() error {
				called = true
				return nil
			}); err != nil || !called {
				t.Fatalf("follow-up error=%v called=%v", err, called)
			}
		})
	}
}
