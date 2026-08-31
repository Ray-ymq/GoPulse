package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	backendhttp "github.com/Ray-ymq/GoPulse/backend/internal/http"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

const shutdownTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		log.Printf("backend stopped with error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	mysqlClient, err := platform.NewMySQL(cfg.MySQL)
	if err != nil {
		return errors.New("initialize MySQL client")
	}
	defer closeResource("MySQL", mysqlClient.Close)

	redisClient := platform.NewRedis(cfg.Redis)
	defer closeResource("Redis", redisClient.Close)

	rabbitMQChecker, err := platform.NewRabbitMQ(cfg.RabbitMQURL)
	if err != nil {
		return errors.New("initialize RabbitMQ checker")
	}

	router := backendhttp.NewRouter(backendhttp.Dependencies{
		MySQL:    mysqlClient,
		Redis:    redisClient,
		RabbitMQ: rabbitMQChecker,
	})
	server := &stdhttp.Server{
		Addr:              cfg.HTTPAddress(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	return serve(signalContext, server, server.ListenAndServe)
}

func serve(ctx context.Context, server *stdhttp.Server, startServer func() error) error {
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- startServer()
	}()

	log.Printf("backend listening on %s", server.Addr)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("HTTP server shutdown failed: %w", err)
		}

		if err := <-serverErrors; err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed during shutdown: %w", err)
		}
		return nil
	}
}

func closeResource(name string, close func() error) {
	if err := close(); err != nil {
		log.Printf("%s resource close failed", name)
	}
}
