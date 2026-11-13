package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	defaultHTTPMaxConcurrency          = 128
	defaultRateLimitLocalMaxIdentities = 1024
	defaultResourceRead                = 64
	defaultResourceWrite               = 32
	defaultResourceDatabase            = 32
	defaultResourceRebuild             = 8
	defaultResourceWaiter              = 256
	minimumRateLimitLocalMaxIdentities = 1
	maximumRateLimitLocalMaxIdentities = 100000
	minimumHTTPMaxConcurrency          = 1
	maximumHTTPMaxConcurrency          = 1024
)

// ProtectionConfig is deliberately static. It is loaded once with the
// process configuration and is never consulted per request.
type ProtectionConfig struct {
	RateLimitEnabled          bool
	RateLimitOperationTimeout time.Duration
	LocalMaxIdentities        int
	ResourceAuth              int
	ResourceRead              int
	ResourceWrite             int
	ResourceDatabase          int
	ResourceRebuild           int
	ResourceWaiter            int
	TrustedProxyCIDRs         []string
}

func loadProtectionConfig(lookup LookupFunc, redisTimeout time.Duration) (ProtectionConfig, error) {
	protection := ProtectionConfig{}
	enabled, err := booleanValue(lookup, "BACKEND_RATE_LIMIT_ENABLED", true)
	if err != nil {
		return ProtectionConfig{}, err
	}
	operationTimeout, err := durationValue(lookup, "BACKEND_RATE_LIMIT_OPERATION_TIMEOUT", redisTimeout, minimumRedisOperationTimeout, maximumRedisOperationTimeout)
	if err != nil {
		return ProtectionConfig{}, err
	}
	localMax, err := integerValue(lookup, "BACKEND_RATE_LIMIT_LOCAL_MAX_IDENTITIES", defaultRateLimitLocalMaxIdentities)
	if err != nil || localMax < minimumRateLimitLocalMaxIdentities || localMax > maximumRateLimitLocalMaxIdentities {
		return ProtectionConfig{}, fmt.Errorf("BACKEND_RATE_LIMIT_LOCAL_MAX_IDENTITIES must be between %d and %d", minimumRateLimitLocalMaxIdentities, maximumRateLimitLocalMaxIdentities)
	}
	values := []struct {
		key      string
		fallback int
		assign   func(int)
	}{
		{"BACKEND_RESOURCE_AUTH", defaultResourceAuth, func(value int) { protection.ResourceAuth = value }},
		{"BACKEND_RESOURCE_READ", defaultResourceRead, func(value int) { protection.ResourceRead = value }},
		{"BACKEND_RESOURCE_WRITE", defaultResourceWrite, func(value int) { protection.ResourceWrite = value }},
		{"BACKEND_RESOURCE_DATABASE", defaultResourceDatabase, func(value int) { protection.ResourceDatabase = value }},
		{"BACKEND_RESOURCE_REBUILD", defaultResourceRebuild, func(value int) { protection.ResourceRebuild = value }},
		{"BACKEND_RESOURCE_WAITER", defaultResourceWaiter, func(value int) { protection.ResourceWaiter = value }},
	}
	for _, item := range values {
		value, valueErr := integerValue(lookup, item.key, item.fallback)
		if valueErr != nil || value < 1 || value > maximumHTTPMaxConcurrency*4 {
			return ProtectionConfig{}, fmt.Errorf("%s must be between 1 and %d", item.key, maximumHTTPMaxConcurrency*4)
		}
		item.assign(value)
	}
	protection.RateLimitEnabled = enabled
	protection.RateLimitOperationTimeout = operationTimeout
	protection.LocalMaxIdentities = localMax
	rawProxies := valueOrDefault(lookup, "BACKEND_TRUSTED_PROXY_CIDRS", "")
	if rawProxies != "" {
		for _, raw := range strings.Split(rawProxies, ",") {
			cidr := strings.TrimSpace(raw)
			if cidr == "" {
				return ProtectionConfig{}, errors.New("BACKEND_TRUSTED_PROXY_CIDRS contains an empty entry")
			}
			if _, _, parseErr := net.ParseCIDR(cidr); parseErr != nil {
				return ProtectionConfig{}, errors.New("BACKEND_TRUSTED_PROXY_CIDRS must contain CIDR networks")
			}
			protection.TrustedProxyCIDRs = append(protection.TrustedProxyCIDRs, cidr)
		}
	}
	return protection, nil
}

func httpMaxConcurrencyKey(role ServiceRole) string {

	return "BACKEND_HTTP_MAX_CONCURRENCY"
}

func httpMaxConcurrencyValue(lookup LookupFunc, role ServiceRole) (int, error) {

	return integerValue(lookup, httpMaxConcurrencyKey(role), defaultHTTPMaxConcurrency)
}
