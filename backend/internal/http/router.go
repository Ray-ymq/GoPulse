package http

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"io"
	"log/slog"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/gin-gonic/gin"
)

const (
	defaultCheckerTimeout = time.Second
	defaultRequestTimeout = 1500 * time.Millisecond
)

type Checker interface {
	Check(context.Context) error
}

type Dependencies struct {
	Probes             *componentmetrics.Probes
	MySQL              Checker
	Redis              Checker
	RabbitMQ           Checker
	Elasticsearch      Checker
	Logger             *slog.Logger
	RequestIDGenerator middleware.RequestIDGenerator
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
			componentmetrics.BackendActive().ObserveRequest(c.Request.Method, c.FullPath(), c.Writer.Status(), time.Since(started))
		},
		middleware.RequestID(httpLogger, dependencies.RequestIDGenerator),
		middleware.Access(httpLogger),
		middleware.Recovery(httpLogger),
		middleware.SameOrigin(),
	)
	probes := dependencies.Probes
	if probes == nil {
		probes, _ = componentmetrics.NewProbes(context.Background(), checkerTimeout, 250*time.Millisecond, func(ctx context.Context) error {
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
				componentmetrics.WriteError(c.Writer, status, code, message)
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
