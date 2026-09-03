package http

import (
	"context"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/gin-gonic/gin"
)

const (
	defaultCheckerTimeout = time.Second
	defaultRequestTimeout = 1500 * time.Millisecond
)

const (
	statusUp   = "up"
	statusDown = "down"
)

type Checker interface {
	Check(context.Context) error
}

type Dependencies struct {
	MySQL              Checker
	Redis              Checker
	RabbitMQ           Checker
	Elasticsearch      Checker
	Logger             *slog.Logger
	RequestIDGenerator middleware.RequestIDGenerator
}

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

type readinessChecks struct {
	MySQL         string `json:"mysql"`
	Redis         string `json:"redis"`
	RabbitMQ      string `json:"rabbitmq"`
	Elasticsearch string `json:"elasticsearch"`
}

type readinessResponse struct {
	Status  string          `json:"status"`
	Service string          `json:"service"`
	Checks  readinessChecks `json:"checks"`
}

type checkerResult struct {
	name   string
	status string
}

// checkerRunner allows at most one in-flight execution for a dependency. A
// checker that ignores context can occupy its slot, but repeated readiness
// requests cannot create an unbounded number of background goroutines.
type checkerRunner struct {
	name    string
	checker Checker
	slot    chan struct{}
}

func newCheckerRunner(name string, checker Checker) *checkerRunner {
	return &checkerRunner{name: name, checker: checker, slot: make(chan struct{}, 1)}
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
		middleware.RequestID(httpLogger, dependencies.RequestIDGenerator),
		middleware.Access(httpLogger),
		middleware.Recovery(httpLogger),
	)
	router.GET("/health", healthHandler)
	router.GET("/ready", readinessHandler(dependencies, checkerTimeout, requestTimeout))

	var apiRoutes APIRoutes
	if len(routes) > 0 {
		apiRoutes = routes[0]
	}
	registerAPIV1Routes(router, apiRoutes)
	return router
}

func healthHandler(c *gin.Context) {
	c.JSON(stdhttp.StatusOK, healthResponse{
		Status:  "ok",
		Service: "backend",
	})
}

func readinessHandler(dependencies Dependencies, checkerTimeout, requestTimeout time.Duration) gin.HandlerFunc {
	runners := []*checkerRunner{
		newCheckerRunner("mysql", dependencies.MySQL),
		newCheckerRunner("redis", dependencies.Redis),
		newCheckerRunner("rabbitmq", dependencies.RabbitMQ),
		newCheckerRunner("elasticsearch", dependencies.Elasticsearch),
	}

	return func(c *gin.Context) {
		requestContext, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
		defer cancel()

		results := make(chan checkerResult, len(runners))
		var workers sync.WaitGroup
		workers.Add(len(runners))
		for _, runner := range runners {
			go func(runner *checkerRunner) {
				defer workers.Done()
				results <- runner.run(requestContext, checkerTimeout)
			}(runner)
		}

		go func() {
			workers.Wait()
			close(results)
		}()

		statuses := map[string]string{
			"mysql":         statusDown,
			"redis":         statusDown,
			"rabbitmq":      statusDown,
			"elasticsearch": statusDown,
		}

		remaining := len(runners)
	collect:
		for remaining > 0 {
			select {
			case result, ok := <-results:
				if !ok {
					break collect
				}
				statuses[result.name] = result.status
				remaining--
			case <-requestContext.Done():
				break collect
			}
		}

		responseStatus := "ready"
		httpStatus := stdhttp.StatusOK
		if statuses["mysql"] != statusUp || statuses["redis"] != statusUp || statuses["rabbitmq"] != statusUp || statuses["elasticsearch"] != statusUp {
			responseStatus = "not_ready"
			httpStatus = stdhttp.StatusServiceUnavailable
		}

		c.JSON(httpStatus, readinessResponse{
			Status:  responseStatus,
			Service: "backend",
			Checks: readinessChecks{
				MySQL:         statuses["mysql"],
				Redis:         statuses["redis"],
				RabbitMQ:      statuses["rabbitmq"],
				Elasticsearch: statuses["elasticsearch"],
			},
		})
	}
}

func (runner *checkerRunner) run(parent context.Context, timeout time.Duration) checkerResult {
	result := checkerResult{name: runner.name, status: statusDown}
	if runner.checker == nil {
		return result
	}

	select {
	case runner.slot <- struct{}{}:
	case <-parent.Done():
		return result
	default:
		return result
	}

	checkContext, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	finished := make(chan bool, 1)
	go func() {
		healthy := executeChecker(checkContext, runner.checker)
		<-runner.slot
		finished <- healthy
	}()

	select {
	case healthy := <-finished:
		if healthy {
			result.status = statusUp
		}
	case <-checkContext.Done():
	}
	return result
}

func executeChecker(ctx context.Context, checker Checker) (healthy bool) {
	defer func() {
		if recover() != nil {
			healthy = false
		}
	}()
	return checker.Check(ctx) == nil && ctx.Err() == nil
}
