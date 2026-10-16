package middleware

import (
	"context"
	"errors"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/ratelimit"
	"github.com/gin-gonic/gin"
)

type protectionLimiter struct {
	decision ratelimit.Decision
	err      error
	identity string
}

func (limiter *protectionLimiter) Allow(_ context.Context, _ ratelimit.Policy, identity string) (ratelimit.Decision, error) {
	limiter.identity = identity
	return limiter.decision, limiter.err
}

func protectionPolicy(name string) ratelimit.Policy {
	return ratelimit.Policy{Name: name, KeyPrefix: "gopulse:rl:v1:" + name + ":", Capacity: 1, RefillPerSecond: 1, Cost: 1, IdleTTL: time.Minute, CreationCapacity: 1, CreationRate: 1, CreationTTL: time.Minute}
}

func TestRequestSourceIgnoresUntrustedForwardedHeader(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.20")
	source, err := RequestSource(request, nil)
	if err != nil || source != "192.0.2.10" {
		t.Fatalf("source=%q error=%v", source, err)
	}
	_, network, _ := net.ParseCIDR("192.0.2.0/24")
	source, err = RequestSource(request, []*net.IPNet{network})
	if err != nil || source != "198.51.100.20" {
		t.Fatalf("trusted source=%q error=%v", source, err)
	}
}

func TestRateLimitMiddlewareReturns429WithRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := &protectionLimiter{decision: ratelimit.Decision{RetryAfter: 1500 * time.Millisecond, Reason: "rate_limited", Mode: "shared"}}
	handler, err := NewRateLimit(RateLimitConfig{Shared: limiter, Policies: map[string]ratelimit.Policy{PolicyPostRead: protectionPolicy(PolicyPostRead)}})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(handler)
	router.GET("/api/v1/posts/:postId", func(c *gin.Context) { c.Status(stdhttp.StatusNoContent) })
	result := httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/posts/31", nil))
	if result.Code != stdhttp.StatusTooManyRequests || result.Header().Get("Retry-After") != "2" || limiter.identity != "ip:192.0.2.1" {
		t.Fatalf("status=%d retry=%q identity=%q body=%s", result.Code, result.Header().Get("Retry-After"), limiter.identity, result.Body.String())
	}
}

func TestRateLimitReadFallsBackLocallyButWriteFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	shared := &protectionLimiter{err: errors.New("redis unavailable")}
	local := &protectionLimiter{decision: ratelimit.Decision{Allowed: true, Mode: "local", Reason: "allow"}}
	readHandler, err := NewRateLimit(RateLimitConfig{Shared: shared, Local: local, Policies: map[string]ratelimit.Policy{PolicyPostRead: protectionPolicy(PolicyPostRead), PolicyFactWrite: protectionPolicy(PolicyFactWrite)}})
	if err != nil {
		t.Fatal(err)
	}
	read := gin.New()
	read.Use(readHandler)
	read.GET("/api/v1/posts/:postId", func(c *gin.Context) { c.Status(stdhttp.StatusNoContent) })
	result := httptest.NewRecorder()
	read.ServeHTTP(result, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/posts/31", nil))
	if result.Code != stdhttp.StatusNoContent || local.identity != "ip:192.0.2.1" {
		t.Fatalf("read status=%d identity=%q", result.Code, local.identity)
	}
	write := gin.New()
	write.Use(readHandler)
	write.POST("/api/v1/posts", func(c *gin.Context) { c.Status(stdhttp.StatusNoContent) })
	result = httptest.NewRecorder()
	write.ServeHTTP(result, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/posts", nil))
	if result.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("write status=%d body=%s", result.Code, result.Body.String())
	}
}
