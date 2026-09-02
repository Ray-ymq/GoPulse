package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/search"
)

func main() {
	ifMissing := flag.Bool("if-missing", false, "initialize the search alias only when it does not exist")
	flag.Parse()
	if flag.NArg() != 0 {
		log.Fatal("search reindex accepts no positional arguments")
	}
	cfg, err := config.LoadReindex()
	if err != nil {
		log.Fatalf("load search reindex configuration: %v", err)
	}
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		log.Fatal("open MySQL for search reindex")
	}
	defer database.Close()
	elasticsearch, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		log.Fatal("create Elasticsearch client for search reindex")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reindexer := search.NewReindexer(
		search.NewReindexStore(database),
		search.NewElasticsearchRepository(elasticsearch),
		cfg.Elasticsearch.ReindexBatch,
	)
	if err := reindexer.Run(ctx, *ifMissing); err != nil {
		log.Fatalf("search reindex failed: %v", err)
	}
	fmt.Println("search reindex completed")
}
