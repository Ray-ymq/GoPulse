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
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/search"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Printf("search indexer stopped with error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadSearchIndexer()
	if err != nil {
		return fmt.Errorf("load search indexer configuration: %w", err)
	}
	mysqlClient, err := platform.NewMySQL(cfg.MySQL)
	if err != nil {
		return errors.New("initialize search indexer MySQL client")
	}
	defer func() {
		if err := mysqlClient.Close(); err != nil {
			log.Printf("search indexer MySQL close failed")
		}
	}()
	elasticsearchClient, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		return errors.New("initialize search indexer Elasticsearch client")
	}
	processor, err := search.NewProcessor(search.NewMySQLDocumentStore(mysqlClient.DB()), search.NewElasticsearchRepository(elasticsearchClient))
	if err != nil {
		return errors.New("initialize search processor")
	}
	runtime, err := worker.NewRuntime(cfg.RabbitMQURL, processor, worker.RuntimeOptions{
		Profile:  worker.SearchProfile,
		Prefetch: cfg.Worker.Prefetch, MaxRetries: cfg.Worker.MaxRetries,
		RetryDelay: cfg.Worker.RetryDelay, PublishTimeout: cfg.Worker.PublishTimeout,
		ShutdownTimeout:  cfg.Worker.ShutdownTimeout,
		ReconnectMinimum: cfg.Worker.ReconnectMinimum, ReconnectMaximum: cfg.Worker.ReconnectMaximum,
	})
	if err != nil {
		return errors.New("initialize search indexer runtime")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("search indexer started")
	return runtime.Run(ctx)
}
