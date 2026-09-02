//go:build integration

package integrationtest

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"
)

const postFactsLockName = "gopulse_integration_post_facts"

// AcquirePostFactsLock serializes integration tests that create globally
// visible posts across independently executing Go package test binaries.
// MySQL named locks are connection-scoped, so the reserved connection remains
// open until the returned release function completes.
func AcquirePostFactsLock(t *testing.T, database *sql.DB) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve MySQL integration lock connection: %v", err)
	}

	// Poll with a non-blocking GET_LOCK call instead of waiting inside MySQL.
	// Production connections intentionally use a one-second read timeout, while
	// separate Go package test binaries can hold this lock for several seconds.
	for {
		var acquired sql.NullInt64
		if err := connection.QueryRowContext(ctx, `SELECT GET_LOCK(?, 0)`, postFactsLockName).Scan(&acquired); err != nil {
			_ = connection.Close()
			t.Fatalf("acquire MySQL integration post-facts lock: %v", err)
		}
		if acquired.Valid && acquired.Int64 == 1 {
			break
		}
		select {
		case <-ctx.Done():
			_ = connection.Close()
			t.Fatalf("acquire MySQL integration post-facts lock: %v", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			releaseContext, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer releaseCancel()
			var released sql.NullInt64
			if err := connection.QueryRowContext(releaseContext, `SELECT RELEASE_LOCK(?)`, postFactsLockName).Scan(&released); err != nil {
				t.Errorf("release MySQL integration post-facts lock: %v", err)
			} else if !released.Valid || released.Int64 != 1 {
				t.Errorf("release MySQL integration post-facts lock returned %#v", released)
			}
			if err := connection.Close(); err != nil {
				t.Errorf("close MySQL integration lock connection: %v", err)
			}
		})
	}
}
