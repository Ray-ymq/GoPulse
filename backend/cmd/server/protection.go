package main

import (
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/ratelimit"
)

// protectionPolicies is the initial candidate set. The feature flag keeps
// the candidate closed until the Phase 1 baseline and resource contract are
// accepted; changing values requires a new configuration review.
func protectionPolicies(cacheTTL time.Duration) map[string]ratelimit.Policy {
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}
	return map[string]ratelimit.Policy{
		middleware.PolicyAuthSource: {
			Name: "auth_source", KeyPrefix: "gopulse:rl:v1:auth_source:ip:", Capacity: 30, RefillPerSecond: 0.5, Cost: 1,
			IdleTTL: 10 * time.Minute, CreationCapacity: 1000, CreationRate: 10, CreationTTL: 10 * time.Minute,
		},
		middleware.PolicyLoginAttempt: {
			Name: "login_attempt", KeyPrefix: "gopulse:rl:v1:login_attempt:source:", Capacity: 10, RefillPerSecond: 0.2, Cost: 1,
			IdleTTL: 10 * time.Minute, CreationCapacity: 1000, CreationRate: 10, CreationTTL: 10 * time.Minute,
		},
		middleware.PolicyPostRead: {
			Name: "post_read", KeyPrefix: "gopulse:rl:v1:post_read:", Capacity: 60, RefillPerSecond: 10, Cost: 1,
			IdleTTL: cacheTTL + time.Minute, CreationCapacity: 5000, CreationRate: 100, CreationTTL: 10 * time.Minute,
			// The read path is the only high-volume quota. Reserving four tokens
			// per shared decision removes three round trips from four requests;
			// at most three tokens can be stranded, which is 0.3s of refill.
			Lease: 4,
		},
		middleware.PolicyFactWrite: {
			Name: "fact_write", KeyPrefix: "gopulse:rl:v1:fact_write:", Capacity: 20, RefillPerSecond: 2, Cost: 1,
			IdleTTL: 10 * time.Minute, CreationCapacity: 1000, CreationRate: 10, CreationTTL: 10 * time.Minute,
		},
		middleware.PolicyInteraction: {
			Name: "interaction_like", KeyPrefix: "gopulse:rl:v1:interaction_like:", Capacity: 10, RefillPerSecond: 1, Cost: 1,
			IdleTTL: 10 * time.Minute, CreationCapacity: 1000, CreationRate: 10, CreationTTL: 10 * time.Minute,
		},
	}
}
