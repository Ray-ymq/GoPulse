package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Printf("business worker stopped with error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadWorker()
	if err != nil {
		return fmt.Errorf("load worker configuration: %w", err)
	}
	mysqlClient, err := platform.NewMySQL(cfg.MySQL)
	if err != nil {
		return errors.New("initialize worker MySQL client")
	}
	defer func() {
		if err := mysqlClient.Close(); err != nil {
			log.Printf("business worker MySQL close failed")
		}
	}()
	repository, err := notification.NewRepository(mysqlClient.DB())
	if err != nil {
		return errors.New("initialize notification repository")
	}
	processor, err := notification.NewProcessor(repository)
	if err != nil {
		return errors.New("initialize notification processor")
	}
	runtime, err := worker.NewRuntime(cfg.RabbitMQURL, processor, worker.RuntimeOptions{
		Profile:  worker.BusinessProfile,
		Prefetch: cfg.Worker.Prefetch, MaxRetries: cfg.Worker.MaxRetries,
		RetryDelay: cfg.Worker.RetryDelay, PublishTimeout: cfg.Worker.PublishTimeout,
		ShutdownTimeout:  cfg.Worker.ShutdownTimeout,
		ReconnectMinimum: cfg.Worker.ReconnectMinimum, ReconnectMaximum: cfg.Worker.ReconnectMaximum,
	})
	if err != nil {
		return errors.New("initialize business worker runtime")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("business worker started")
	return runtime.Run(ctx)
}
