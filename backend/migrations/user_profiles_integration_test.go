//go:build integration

package migrations

import (
	"context"
	"fmt"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"strings"
	"testing"
	"time"
)

func TestIntegrationProfileMigrationRoundTrip(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	table := fmt.Sprintf("users_profile_migration_%d", time.Now().UnixNano())
	if _, err = db.ExecContext(ctx, "CREATE TABLE "+table+" (id BIGINT PRIMARY KEY, username VARCHAR(32) NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, "DROP TABLE "+table)
	if _, err = db.ExecContext(ctx, "INSERT INTO "+table+" VALUES (1,'existing')"); err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"up", "down", "up"} {
		sql, err := files.ReadFile("000006_user_profiles." + direction + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(strings.ReplaceAll(string(sql), "users", table), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err = db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		if direction == "up" {
			var name, bio string
			if err = db.QueryRowContext(ctx, "SELECT display_name,bio FROM "+table).Scan(&name, &bio); err != nil || name != "existing" || bio != "" {
				t.Fatalf("backfill %q %q %v", name, bio, err)
			}
		}
	}
}
