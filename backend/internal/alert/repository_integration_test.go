//go:build integration

package alert

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationOwnedMySQLStateAndLease(t *testing.T) {
	cfg := integrationtest.Environment(t)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	actor, cleanup := ensureIntegrationSuperAdmin(t, database)
	defer cleanup()
	_ = actor
	assertOwnedMySQLStateAndLease(t, database)
}

func ensureIntegrationSuperAdmin(t *testing.T, database *sql.DB) (uint64, func()) {
	t.Helper()
	var actor uint64
	err := database.QueryRow(`SELECT user_id FROM bootstrap_super_admin WHERE singleton=1`).Scan(&actor)
	if err == nil {
		return actor, func() {}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find bootstrap super admin: %v", err)
	}
	username := fmt.Sprintf("p22_alert_%d", time.Now().UnixNano()%10000000000)
	result, err := database.Exec(`INSERT INTO users(username,password_hash,role) VALUES (?, ?, 'super_admin')`, username, "not-a-login")
	if err != nil {
		t.Fatalf("create alert integration super admin: %v", err)
	}
	lastID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read alert integration super admin id: %v", err)
	}
	actor = uint64(lastID)
	if _, err := database.Exec(`INSERT INTO bootstrap_super_admin(singleton,user_id) VALUES (1, ?)`, actor); err != nil {
		t.Fatalf("claim bootstrap super admin: %v", err)
	}
	return uint64(actor), func() {
		_, _ = database.Exec(`DELETE FROM bootstrap_super_admin WHERE singleton=1 AND user_id=?`, actor)
		_, _ = database.Exec(`DELETE FROM users WHERE id=?`, actor)
	}
}
