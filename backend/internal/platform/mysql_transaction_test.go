package platform

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestMySQLTransactionRecoveryBoundaries(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       error
		safe      bool
		recovers  bool
		wantCalls int
	}{
		{"connection_recovers", driver.ErrBadConn, true, true, 2},
		{"connection_exhausted", mysql.ErrInvalidConn, true, false, 2},
		{"read_timeout", &net.DNSError{IsTimeout: true}, true, true, 2},
		{"deadlock", &mysql.MySQLError{Number: 1213}, true, true, 2},
		{"lock_timeout", &mysql.MySQLError{Number: 1205}, true, true, 2},
		{"foreign_key_error", &mysql.MySQLError{Number: 1452}, true, false, 1},
		{"unknown_commit", driver.ErrBadConn, false, false, 1},
		{"cancelled_operation", context.Canceled, true, false, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var firstContext context.Context
			err := RunMySQLTransaction(context.Background(), func(ctx context.Context) (bool, error) {
				calls++
				if calls == 1 {
					firstContext = ctx
				} else if ctx != firstContext {
					t.Fatal("retry replaced the shared transaction deadline")
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 3*time.Second {
					t.Fatal("transaction recovery is not bounded")
				}
				if test.recovers && calls == 2 {
					return false, nil
				}
				return test.safe, fmt.Errorf("database operation: %w", test.err)
			})
			if calls != test.wantCalls || (err == nil) != test.recovers {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestMySQLTransactionRecoveryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := RunMySQLTransaction(ctx, func(context.Context) (bool, error) { calls++; return true, driver.ErrBadConn })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("cancelled context: calls=%d error=%v", calls, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	err = RunMySQLTransaction(ctx, func(context.Context) (bool, error) {
		calls++
		cancel()
		return true, driver.ErrBadConn
	})
	if !errors.Is(err, driver.ErrBadConn) || calls != 1 {
		t.Fatalf("cancelled retry: calls=%d error=%v", calls, err)
	}
}
