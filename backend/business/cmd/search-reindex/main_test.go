package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
)

func TestExecuteRejectsArgumentsBeforeLoadingConfiguration(t *testing.T) {
	var stdout bytes.Buffer
	loaded := false
	code := execute([]string{"--unknown"}, &stdout, func() (config.ReindexConfig, error) {
		loaded = true
		return config.ReindexConfig{}, nil
	}, func(config.ReindexConfig, bool, *slog.Logger) error { return nil })
	if code != 1 || loaded {
		t.Fatalf("code=%d loaded=%v", code, loaded)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"reason":"invalid_arguments"`)) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestValidateReindexElasticsearchRejectsObservationClient(t *testing.T) {
	if err := validateReindexElasticsearch(config.ReindexConfig{Elasticsearch: config.ElasticsearchConfig{Purpose: "observability"}}); err == nil {
		t.Fatal("observation Elasticsearch client accepted by search reindex")
	}
	if err := validateReindexElasticsearch(config.ReindexConfig{Elasticsearch: config.ElasticsearchConfig{Purpose: "search"}}); err != nil {
		t.Fatalf("business search client rejected: %v", err)
	}
}

func TestParseReindexArgumentsRequiresCompleteRestoreIdentity(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	validOld := "123e4567-e89b-12d3-a456-426614174000"
	validNew := "123e4567-e89b-12d3-a456-426614174001"
	operation := strings.Repeat("a", 64)
	options, err := parseReindexArguments([]string{"--restore-operation", operation, "--expected-old-epoch", validOld, "--new-epoch", validNew}, logger)
	if err != nil || options.restoreOperation != operation || options.expectedOldEpoch != validOld || options.newEpoch != validNew || options.ifMissing {
		t.Fatalf("options=%#v err=%v", options, err)
	}
	for _, arguments := range [][]string{
		{"--restore-operation", operation},
		{"--expected-old-epoch", validOld, "--new-epoch", validNew},
		{"--restore-operation", operation, "--expected-old-epoch", validOld, "--new-epoch", validOld},
		{"--if-missing", "--restore-operation", operation, "--expected-old-epoch", validOld, "--new-epoch", validNew},
	} {
		if _, err := parseReindexArguments(arguments, logger); err == nil {
			t.Fatalf("accepted invalid restore arguments %v", arguments)
		}
	}
}
