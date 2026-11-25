package post

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/async/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/modules/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform/readcache"
)

type database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type transactionStarter interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type RepositoryOptions struct {
	Outbox        outbox.Writer
	BusinessState *businessstate.Repository
	Clock         func() time.Time
	Logger        *slog.Logger
}

type MySQLRepository struct {
	database              database
	outbox                outbox.Writer
	state                 *businessstate.Repository
	counts                *readcache.Cache[uint64, interactionCounts]
	discovery             *discoveryCache
	invalidateCommentPage func(context.Context, string, uint64)
	clock                 func() time.Time
	logger                *slog.Logger
}

func NewMySQLRepository(database database, options ...RepositoryOptions) *MySQLRepository {
	repository := &MySQLRepository{database: database, clock: time.Now, logger: logging.Discard("backend")}
	if len(options) > 0 {
		repository.outbox = options[0].Outbox
		repository.state = options[0].BusinessState
		if options[0].Logger != nil {
			repository.logger = logging.Module(options[0].Logger, "post")
		}
		if options[0].Clock != nil {
			repository.clock = options[0].Clock
		}
	}
	return repository
}

func NewMySQLRepositoryWithOutbox(database database, eventOutbox outbox.Writer) *MySQLRepository {
	return NewMySQLRepository(database, RepositoryOptions{Outbox: eventOutbox})
}
