//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

func TestIntegrationPromoteCommandPersistsAdminRoleAndIsIdempotent(t *testing.T) {
	cfg := integrationtest.Environment(t)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()

	username := fmt.Sprintf("AdminCLI_%012d", time.Now().UnixNano()%1_000_000_000_000)
	if _, err := database.Exec(`INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, "$2a$10$integration-placeholder"); err != nil {
		t.Fatalf("insert command user: %v", err)
	}
	defer func() { _, _ = database.Exec(`DELETE FROM users WHERE username = ?`, username) }()

	repository := user.NewMySQLRepository(database)
	open := func() (rolePromoter, func(), error) { return repository, func() {}, nil }
	for attempt := 0; attempt < 2; attempt++ {
		var output bytes.Buffer
		if err := run([]string{"promote", "--username", " " + username + " "}, &output, open); err != nil {
			t.Fatalf("run() attempt %d error = %v", attempt+1, err)
		}
		if output.String() != "administrator role ensured\n" {
			t.Fatalf("run() attempt %d output = %q", attempt+1, output.String())
		}
	}

	var role string
	if err := database.QueryRow(`SELECT role FROM users WHERE username = ?`, username).Scan(&role); err != nil || role != "admin" {
		t.Fatalf("stored role = %q, %v; want admin", role, err)
	}
	if err := run([]string{"promote", "--username", "MissingCLIUser"}, &bytes.Buffer{}, open); err == nil || err.Error() != "registered user was not found" {
		t.Fatalf("missing user error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	record, err := repository.FindByUsername(ctx, username)
	if err != nil || record.Role != user.RoleAdmin {
		t.Fatalf("repository role = %q, %v; want admin", record.Role, err)
	}
}
