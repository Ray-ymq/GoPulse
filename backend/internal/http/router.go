package http

import (
	"context"
	stdhttp "net/http"
	"sync"
	"time"

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
	MySQL    Checker
	Redis    Checker
	RabbitMQ Checker
}

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

type readinessChecks struct {
	MySQL    string `json:"mysql"`
	Redis    string `json:"redis"`
	RabbitMQ string `json:"rabbitmq"`
}

type readinessResponse struct {
	Status  string          `json:"status"`
	Service string          `json:"service"`
	Checks  readinessChecks `json:"checks"`
}

type namedChecker struct {
	name    string
	checker Checker
}

type checkerResult struct {
	name   string
	status string
}

func NewRouter(dependencies Dependencies) *gin.Engine {
	return newRouter(dependencies, defaultCheckerTimeout, defaultRequestTimeout)
}

func newRouter(dependencies Dependencies, checkerTimeout, requestTimeout time.Duration) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/health", healthHandler)
	router.GET("/ready", readinessHandler(dependencies, checkerTimeout, requestTimeout))
	return router
}

func healthHandler(c *gin.Context) {
	c.JSON(stdhttp.StatusOK, healthResponse{
		Status:  "ok",
		Service: "backend",
	})
}

func readinessHandler(dependencies Dependencies, checkerTimeout, requestTimeout time.Duration) gin.HandlerFunc {
	checkers := []namedChecker{
		{name: "mysql", checker: dependencies.MySQL},
		{name: "redis", checker: dependencies.Redis},
		{name: "rabbitmq", checker: dependencies.RabbitMQ},
	}

	return func(c *gin.Context) {
		requestContext, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
		defer cancel()

		results := make(chan checkerResult, len(checkers))
		var workers sync.WaitGroup
		workers.Add(len(checkers))
		for _, item := range checkers {
			go func(item namedChecker) {
				defer workers.Done()
				results <- runChecker(requestContext, item, checkerTimeout)
			}(item)
		}

		go func() {
			workers.Wait()
			close(results)
		}()

		statuses := map[string]string{
			"mysql":    statusDown,
			"redis":    statusDown,
			"rabbitmq": statusDown,
		}

		remaining := len(checkers)
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
		if statuses["mysql"] != statusUp || statuses["redis"] != statusUp || statuses["rabbitmq"] != statusUp {
			responseStatus = "not_ready"
			httpStatus = stdhttp.StatusServiceUnavailable
		}

		c.JSON(httpStatus, readinessResponse{
			Status:  responseStatus,
			Service: "backend",
			Checks: readinessChecks{
				MySQL:    statuses["mysql"],
				Redis:    statuses["redis"],
				RabbitMQ: statuses["rabbitmq"],
			},
		})
	}
}

func runChecker(parent context.Context, item namedChecker, timeout time.Duration) checkerResult {
	result := checkerResult{name: item.name, status: statusDown}
	if item.checker == nil {
		return result
	}

	checkContext, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	finished := make(chan error, 1)
	go func() {
		finished <- item.checker.Check(checkContext)
	}()

	select {
	case err := <-finished:
		if err == nil && checkContext.Err() == nil {
			result.status = statusUp
		}
	case <-checkContext.Done():
	}
	return result
}
