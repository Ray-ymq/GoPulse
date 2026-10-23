//go:build integration

package businessstate_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationFactWritersShareEnvironmentAndExcludeRestore(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	state, _ := businessstate.NewRepository(db)
	environment, err := state.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback()
	if _, err := state.LockCurrentTx(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback()
	lockCtx, lockCancel := context.WithTimeout(ctx, time.Second)
	defer lockCancel()
	if _, err := state.LockCurrentTx(lockCtx, second); err != nil {
		t.Fatalf("ordinary writers blocked: %v", err)
	}
	restoreCtx, restoreCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer restoreCancel()
	if err := state.BeginRestore(restoreCtx, strings.Repeat("a", 64), environment.DataEpoch, "123e4567-e89b-12d3-a456-426614174124"); err == nil || errors.Is(err, businessstate.ErrInvalidArgument) || !errors.Is(restoreCtx.Err(), context.DeadlineExceeded) {
		t.Fatalf("restore did not wait for writer locks: %v", err)
	}
}
