package http

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/instrumentation"
	"github.com/Ray-ymq/GoPulse/runtimekit"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/ratelimit"
	"github.com/gin-gonic/gin"
)

const (
	defaultCheckerTimeout = time.Second
	defaultRequestTimeout = 1500 * time.Millisecond
)

type Checker interface {
	Check(context.Context) error
}

// NewRateLimitHandler keeps role assembly on the HTTP package boundary while
// middleware owns source normalization and endpoint failure behavior.
func NewRateLimitHandler(shared, local ratelimit.Limiter, policies map[string]ratelimit.Policy, trustedProxies []*net.IPNet) (gin.HandlerFunc, error) {
	return middleware.NewRateLimit(middleware.RateLimitConfig{
		Shared: shared, Local: local, Policies: policies, TrustedProxies: trustedProxies,
	})
}

type Dependencies struct {
	Probes             *runtimekit.Probes
	MySQL              Checker
	Redis              Checker
	RabbitMQ           Checker
	Elasticsearch      Checker
	Logger             *slog.Logger
	RequestIDGenerator middleware.RequestIDGenerator
	HTTPMaxConcurrency int
}

func ConfigureGinMode(appEnv string) error {
	var mode string
	switch appEnv {
	case "development":
		mode = gin.DebugMode
	case "test":
		mode = gin.TestMode
	case "production":
		mode = gin.ReleaseMode
	default:
		return errors.New("APP_ENV must be one of development, test, or production")
	}

	// GoPulse owns application log formatting. Suppress Gin's debug and error
	// writers so development mode cannot mix framework text into JSON stdout.
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	gin.SetMode(mode)
	return nil
}

func NewRouter(dependencies Dependencies, routes ...APIRoutes) *gin.Engine {
	return newRouter(dependencies, defaultCheckerTimeout, defaultRequestTimeout, routes...)
}

func newRouter(dependencies Dependencies, checkerTimeout, requestTimeout time.Duration, routes ...APIRoutes) *gin.Engine {
	router := gin.New()
	logger := dependencies.Logger
	if logger == nil {
		logger = logging.New("backend", io.Discard)
	}
	httpLogger := logging.Module(logger, "http")
	router.Use(
		func(c *gin.Context) {
			started := time.Now()
			c.Next()
			instrumentation.BackendActive().ObserveRequest(c.Request.Method, c.FullPath(), c.Writer.Status(), time.Since(started))
		},
		middleware.RequestID(httpLogger, dependencies.RequestIDGenerator),
		middleware.Access(httpLogger),
		middleware.Recovery(httpLogger),
		middleware.SameOrigin(),
		middleware.RequestTimeout(requestTimeout),
	)
	if dependencies.HTTPMaxConcurrency > 0 {
		router.Use(apiAdmission(dependencies.HTTPMaxConcurrency))
	}
	probes := dependencies.Probes
	if probes == nil {
		probes, _ = runtimekit.NewProbes(context.Background(), checkerTimeout, 250*time.Millisecond, func(ctx context.Context) error {
			if dependencies.MySQL == nil {
				return errors.New("mysql_unavailable")
			}
			return dependencies.MySQL.Check(ctx)
		})
		probes.Started()
	}
	for _, path := range []string{"/startup", "/live", "/ready", "/health"} {
		router.GET(path, gin.WrapH(probes))
	}
	router.HandleMethodNotAllowed = true
	fallback := func(status int, code, message string) gin.HandlerFunc {
		return func(c *gin.Context) {
			switch c.Request.URL.Path {
			case "/startup", "/live", "/ready", "/health":
				probes.ServeHTTP(c.Writer, c.Request)
			default:
				runtimekit.WriteError(c.Writer, status, code, message)
			}
		}
	}
	router.NoRoute(fallback(404, "not_found", "route not found"))
	router.NoMethod(fallback(405, "method_not_allowed", "method not allowed"))

	var apiRoutes APIRoutes
	if len(routes) > 0 {
		apiRoutes = routes[0]
	}
	registerAPIV1Routes(router, apiRoutes)
	return router
}

func apiAdmission(limit int) gin.HandlerFunc {
	slots := make(chan struct{}, limit)
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path != "/api/v1" && !strings.HasPrefix(path, "/api/v1/") {
			c.Next()
			return
		}
		select {
		case slots <- struct{}{}:
			if metrics := instrumentation.BackendActive(); metrics != nil {
				metrics.ObserveHTTPStarted()
				defer metrics.ObserveHTTPFinished()
			}
			defer func() { <-slots }()
			c.Next()
		default:
			if metrics := instrumentation.BackendActive(); metrics != nil {
				metrics.ObserveHTTPRejected()
			}
			response.Error(c, apperror.New(apperror.CodeBackendBusy, "service temporarily busy"))
			c.Abort()
		}
	}
}
