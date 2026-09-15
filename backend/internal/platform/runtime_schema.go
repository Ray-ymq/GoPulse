package platform

import (
	"context"
	"database/sql"
	"errors"
)

// RequiredSchemaVersion is the shipped schema, not a migration runner. Changing
// schema remains a separate explicit lifecycle operation.
const RequiredSchemaVersion = 13

func CheckRuntimeSchema(ctx context.Context, db *sql.DB) error {
	var version int
	var dirty bool
	if err := db.QueryRowContext(ctx, "SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty); err != nil {
		return errors.New("schema_unavailable")
	}
	if version != RequiredSchemaVersion || dirty {
		return errors.New("schema_not_ready")
	}
	return nil
}
