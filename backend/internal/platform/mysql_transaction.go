package platform

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"time"

	"github.com/go-sql-driver/mysql"
)

const mysqlTransactionRecoveryTimeout = 3 * time.Second

// RunMySQLTransaction permits one retry of a transient database failure within
// one shared deadline. The operation must finish its transaction before
// returning and explicitly say whether replay is safe. An ambiguous COMMIT
// must not be replayed for a non-idempotent write.
func RunMySQLTransaction(ctx context.Context, operation func(context.Context) (safeToRetry bool, err error)) error {
	ctx, cancel := context.WithTimeout(ctx, mysqlTransactionRecoveryTimeout)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		safeToRetry, err := operation(ctx)
		if err == nil || !safeToRetry || attempt == 1 || ctx.Err() != nil || !transientMySQLTransactionError(err) {
			return err
		}
	}
	panic("unreachable MySQL transaction attempt")
}

func transientMySQLTransactionError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysql.ErrInvalidConn) {
		return true
	}
	var databaseError *mysql.MySQLError
	if errors.As(err, &databaseError) {
		return databaseError.Number == 1205 || databaseError.Number == 1213
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}
