package platform

import (
	"context"
	"database/sql"
	"errors"
	migrationfiles "github.com/Ray-ymq/GoPulse/backend/migrations"
)

// Resolve the same embedded inventory as the CLI; never perform implicit DDL.
var requiredSchemaVersion, schemaInventoryError = migrationfiles.Target()

func CheckRuntimeSchema(ctx context.Context, db *sql.DB) error {
	if schemaInventoryError != nil {
		return errors.New("schema_unavailable")
	}
	var version uint
	var dirty bool
	if err := db.QueryRowContext(ctx, "SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty); err != nil {
		return errors.New("schema_unavailable")
	}
	if version != requiredSchemaVersion || dirty {
		return errors.New("schema_not_ready")
	}
	return nil
}
