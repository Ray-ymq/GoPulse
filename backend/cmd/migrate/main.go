package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	migrationfiles "github.com/Ray-ymq/GoPulse/backend/migrations"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Printf("database migration failed: %v", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	if len(args) != 1 || (args[0] != "up" && args[0] != "down") {
		return errors.New("usage: migrate <up|down>")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	database, err := platform.OpenMySQLMigrationDatabase(cfg.MySQL)
	if err != nil {
		return errors.New("initialize MySQL migration connection")
	}
	ownedByMigration := false
	defer func() {
		if !ownedByMigration {
			_ = database.Close()
		}
	}()

	databaseDriver, err := migratemysql.WithInstance(database, &migratemysql.Config{})
	if err != nil {
		return errors.New("initialize MySQL migration driver")
	}
	sourceDriver, err := migrationfiles.Source()
	if err != nil {
		return errors.New("initialize embedded migration source")
	}
	migration, err := migrate.NewWithInstance("iofs", sourceDriver, "mysql", databaseDriver)
	if err != nil {
		return errors.New("initialize migration runner")
	}
	ownedByMigration = true
	defer func() {
		_, _ = migration.Close()
	}()

	switch args[0] {
	case "up":
		err = migration.Up()
	case "down":
		err = migration.Steps(-1)
	}
	if errors.Is(err, migrate.ErrNoChange) {
		_, _ = fmt.Fprintf(output, "database migrations already %s to date\n", directionWord(args[0]))
		return nil
	}
	if err != nil {
		return fmt.Errorf("apply %s migrations: %w", args[0], err)
	}

	_, _ = fmt.Fprintf(output, "database migration %s completed\n", args[0])
	return nil
}

func directionWord(direction string) string {
	if direction == "down" {
		return "rolled back"
	}
	return "up"
}
