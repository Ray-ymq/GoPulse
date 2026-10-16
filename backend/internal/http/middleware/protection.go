package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/ratelimit"
	"github.com/Ray-ymq/GoPulse/backend/internal/resourcebudget"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/gin-gonic/gin"
)

const (
	PolicyAuthSource   = "auth_source"
	PolicyLoginAttempt = "login_attempt"
	PolicyPostRead     = "post_read"
	PolicyFactWrite    = "fact_write"
	PolicyInteraction  = "interaction_like"
)

// RateLimitConfig contains only fixed policy vocabulary. The map is copied at
// construction so request data can never introduce a Redis policy name.
type RateLimitConfig struct {
	Shared         ratelimit.Limiter
	Local          ratelimit.Limiter
	Policies       map[string]ratelimit.Policy
	TrustedProxies []*net.IPNet
}

func NewRateLimit(config RateLimitConfig) (gin.HandlerFunc, error) {
	if config.Shared == nil {
		return nil, errors.New("shared rate limiter is required")
	}
	policies := make(map[string]ratelimit.Policy, len(config.Policies))
	for name, policy := range config.Policies {
		if name != policy.Name || policy.Validate() != nil {
			return nil, fmt.Errorf("invalid rate limit policy %q", name)
		}
		policies[name] = policy
	}
	return func(c *gin.Context) {
		name := policyName(c)
		if name == "" {
			c.Next()
			return
		}
		policy, ok := policies[name]
		if !ok {
			writeRateLimitUnavailable(c)
			return
		}
		identity, err := rateLimitIdentity(c, config.TrustedProxies)
		if err != nil {
			writeRateLimitUnavailable(c)
			return
		}
		decision, err := config.Shared.Allow(c.Request.Context(), policy, identity)
		if err != nil {
			observeRateLimit(name, "shared", "error")
			if name == PolicyPostRead && config.Local != nil {
				decision, err = config.Local.Allow(c.Request.Context(), policy, identity)
				if err == nil {
					decision.Mode = "local"
					observeRateLimit(name, "local", decisionResult(decision))
					c.Set("gopulse.rate_limit_mode", decision.Mode)
					if decision.Allowed {
						c.Next()
						return
					}
					if decision.Reason == "local_capacity" {
						writeBackendBusy(c)
						return
					}
					writeRateLimited(c, decision.RetryAfter)
					return
				}
			}
			writeRateLimitUnavailable(c)
			return
		}
		c.Set("gopulse.rate_limit_mode", decision.Mode)
		observeRateLimit(name, decision.Mode, decisionResult(decision))
		if decision.Allowed {
			c.Next()
			return
		}
		if decision.Reason == "key_creation_limited" {
			writeBackendBusy(c)
			return
		}
		writeRateLimited(c, decision.RetryAfter)
	}, nil
}

func policyName(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	path := c.Request.URL.Path
	if !strings.HasPrefix(path, "/api/v1/") && path != "/api/v1" {
		return ""
	}
	if strings.HasPrefix(path, "/api/v1/auth/") {
		if c.Request.Method == http.MethodPost {
			if strings.HasSuffix(path, "/login") {
				return PolicyLoginAttempt
			}
			return PolicyAuthSource
		}
		return ""
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		return PolicyPostRead
	}
	if strings.HasSuffix(path, "/like") {
		return PolicyInteraction
	}
	return PolicyFactWrite
}

func rateLimitIdentity(c *gin.Context, trusted []*net.IPNet) (string, error) {
	if userID, ok := CurrentUserID(c); ok {
		return "user:" + strconv.FormatUint(userID, 10), nil
	}
	source, err := RequestSource(c.Request, trusted)
	if err != nil {
		return "", err
	}
	if policyName(c) == PolicyLoginAttempt {
		return loginAttemptIdentity(c.Request, source), nil
	}
	return "ip:" + source, nil
}

func loginAttemptIdentity(request *http.Request, source string) string {
	if request == nil || request.Body == nil {
		return "ip:" + source
	}
	payload, err := io.ReadAll(io.LimitReader(request.Body, 4097))
	request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(payload), request.Body))
	if err != nil || len(payload) > 4096 {
		return "ip:" + source
	}
	var credentials struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(payload, &credentials); err != nil {
		return "ip:" + source
	}
	normalized := strings.ToLower(strings.TrimSpace(credentials.Username))
	if normalized == "" || len(normalized) > 128 {
		return "ip:" + source
	}
	digest := sha256.Sum256([]byte(normalized))
	return "subject:" + source + ":" + hex.EncodeToString(digest[:])
}

// RequestSource trusts forwarded addresses only when the direct peer belongs
// to a configured proxy network. An untrusted forwarded header is ignored.
func RequestSource(request *http.Request, trusted []*net.IPNet) (string, error) {
	if request == nil {
		return "", errors.New("request is required")
	}
	peer, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err != nil {
		peer = strings.TrimSpace(request.RemoteAddr)
	}
	peerIP := net.ParseIP(peer)
	if peerIP == nil {
		return "", errors.New("request source is invalid")
	}
	if forwarded := strings.TrimSpace(request.Header.Get("X-Forwarded-For")); forwarded != "" && trustedProxy(peerIP, trusted) {
		first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if parsed := net.ParseIP(first); parsed != nil {
			return parsed.String(), nil
		}
		return "", errors.New("forwarded request source is invalid")
	}
	return peerIP.String(), nil
}

func trustedProxy(value net.IP, trusted []*net.IPNet) bool {
	for _, network := range trusted {
		if network != nil && network.Contains(value) {
			return true
		}
	}
	return false
}

func writeRateLimited(c *gin.Context, retryAfter time.Duration) {
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	seconds := int64((retryAfter + time.Second - 1) / time.Second)
	c.Header("Retry-After", strconv.FormatInt(seconds, 10))
	response.Error(c, apperror.New(apperror.CodeRateLimited, "request rate limit exceeded"))
	c.Abort()
}

func writeRateLimitUnavailable(c *gin.Context) {
	response.Error(c, apperror.New(apperror.CodeRateLimitUnavailable, "request protection temporarily unavailable"))
	c.Abort()
}

func writeBackendBusy(c *gin.Context) {
	response.Error(c, apperror.New(apperror.CodeBackendBusy, "service temporarily busy"))
	c.Abort()
}

// ResourceBudget admits one fixed request category without waiting. It is
// intentionally separate from shared quotas: a valid quota decision does not
// imply that this instance has capacity for the work.
func ResourceBudget(budget *resourcebudget.Budget) gin.HandlerFunc {
	return func(c *gin.Context) {
		if budget == nil || c.Request == nil || !strings.HasPrefix(c.Request.URL.Path, "/api/v1") {
			c.Next()
			return
		}
		kind := requestResourceKind(c)
		release, ok := budget.TryAcquire(kind)
		if !ok {
			observeResourceRejected(kind, "capacity")
			writeBackendBusy(c)
			return
		}
		defer release()
		c.Next()
	}
}

func decisionResult(decision ratelimit.Decision) string {
	if decision.Allowed {
		return "allow"
	}
	return "deny"
}

func observeRateLimit(policy, mode, result string) {
	if metrics := componentmetrics.BackendActive(); metrics != nil {
		metrics.ObserveRateLimit(policy, mode, result)
	}
}

func observeResourceRejected(kind resourcebudget.Kind, reason string) {
	if metrics := componentmetrics.BackendActive(); metrics != nil {
		metrics.ObserveResourceRejected(string(kind), reason)
	}
}

func requestResourceKind(c *gin.Context) resourcebudget.Kind {
	if strings.HasPrefix(c.Request.URL.Path, "/api/v1/auth/") {
		return resourcebudget.Auth
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		return resourcebudget.Read
	}
	return resourcebudget.Write
}

// RequestTimeout puts a bounded deadline around API work. Health and probe
// endpoints remain governed by their own checker timeout.
func RequestTimeout(timeout time.Duration) gin.HandlerFunc {
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	return func(c *gin.Context) {
		if c.Request == nil || !strings.HasPrefix(c.Request.URL.Path, "/api/v1") {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
