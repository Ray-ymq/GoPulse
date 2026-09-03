//go:build integration

package migrations

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationUserRoleMigrationUpDownAndDefaults(t *testing.T) {
	cfg := integrationtest.Environment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()

	table := fmt.Sprintf("users_role_migration_%d", time.Now().UnixNano())
	if _, err := database.ExecContext(ctx, `CREATE TABLE `+table+` (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		username VARCHAR(32) NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		PRIMARY KEY (id),
		UNIQUE KEY uq_username (username)
	) ENGINE=InnoDB`); err != nil {
		t.Fatalf("create isolated users table: %v", err)
	}
	defer func() { _, _ = database.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+table) }()

	if _, err := database.ExecContext(ctx, `INSERT INTO `+table+` (username, password_hash) VALUES ('existing', 'hash')`); err != nil {
		t.Fatalf("insert existing user: %v", err)
	}
	up, err := files.ReadFile("000005_user_roles.up.sql")
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}
	upSQL := strings.Replace(string(up), "ALTER TABLE users", "ALTER TABLE "+table, 1)
	if _, err := database.ExecContext(ctx, upSQL); err != nil {
		t.Fatalf("apply isolated up migration: %v", err)
	}

	var existingRole string
	if err := database.QueryRowContext(ctx, `SELECT role FROM `+table+` WHERE username = 'existing'`).Scan(&existingRole); err != nil || existingRole != "user" {
		t.Fatalf("existing user role = %q, %v; want user", existingRole, err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO `+table+` (username, password_hash) VALUES ('new_user', 'hash')`); err != nil {
		t.Fatalf("insert new user after migration: %v", err)
	}
	var newRole string
	if err := database.QueryRowContext(ctx, `SELECT role FROM `+table+` WHERE username = 'new_user'`).Scan(&newRole); err != nil || newRole != "user" {
		t.Fatalf("new user role = %q, %v; want user", newRole, err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO `+table+` (username, password_hash, role) VALUES ('invalid', 'hash', 'owner')`); err == nil {
		t.Fatal("invalid role insert succeeded")
	}

	down, err := files.ReadFile("000005_user_roles.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	downSQL := strings.Replace(string(down), "ALTER TABLE users", "ALTER TABLE "+table, 1)
	if _, err := database.ExecContext(ctx, downSQL); err != nil {
		t.Fatalf("apply isolated down migration: %v", err)
	}
	var roleColumns int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = ? AND table_name = ? AND column_name = 'role'`, cfg.MySQL.Database, table).Scan(&roleColumns); err != nil {
		t.Fatalf("query role column after down migration: %v", err)
	}
	if roleColumns != 0 {
		t.Fatalf("role columns after down migration = %d, want 0", roleColumns)
	}
}
