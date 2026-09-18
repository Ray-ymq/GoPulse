package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	migrationfiles "github.com/Ray-ymq/GoPulse/backend/migrations"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source"
)

type failure struct {
	Reason string
	Code   int
}

func (e *failure) Error() string         { return e.Reason }
func fail(reason string, code int) error { return &failure{reason, code} }
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		code := 8
		reason := "apply_failure"
		var f *failure
		if errors.As(err, &f) {
			code, reason = f.Code, f.Reason
		}
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"reason": reason, "exit_code": code})
		os.Exit(code)
	}
}

func run(args []string, output io.Writer) error {
	if len(args) != 1 || (args[0] != "validate" && args[0] != "status" && args[0] != "up" && args[0] != "down") {
		return fail("usage: migrate <validate|status|up|down>; down is local development only; product recovery requires backup/restore", 2)
	}
	target, err := migrationfiles.Target()
	if err != nil {
		return fail("invalid_source", 9)
	}
	if args[0] == "validate" {
		return json.NewEncoder(output).Encode(map[string]any{"binary_target": target, "state": "valid"})
	}
	mysqlConfig, err := config.LoadMySQL()
	if err != nil {
		return fail("invalid_config", 2)
	}
	db, err := platform.OpenMySQLMigrationDatabase(mysqlConfig)
	if err != nil {
		return fail("connection_failure", 3)
	}
	defer db.Close()
	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	if err != nil {
		return lockFailure(err)
	}
	defer driver.Close()
	sourceDriver, err := migrationfiles.Source()
	if err != nil {
		return fail("invalid_source", 9)
	}
	defer sourceDriver.Close()
	if args[0] == "down" {
		migration, err := migrate.NewWithInstance("iofs", sourceDriver, "mysql", driver)
		if err != nil {
			return fail("apply_failure", 8)
		}
		if err := migration.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fail("apply_failure", 8)
		}
		return nil
	}
	// Check and apply under one database lock: no preflight/Up race can clear a
	// future or dirty version written by a concurrent runner.
	if err := driver.Lock(); err != nil {
		return lockFailure(err)
	}
	defer driver.Unlock()
	version, dirty, err := driver.Version()
	if err != nil {
		return fail("connection_failure", 3)
	}
	state := schemaState(version, dirty, target)
	if args[0] == "status" {
		if err := json.NewEncoder(output).Encode(map[string]any{"binary_target": target, "database_version": version, "state": state}); err != nil {
			return err
		}
		if state == "dirty" {
			return fail("dirty", 4)
		}
		if state == "ahead" {
			return fail("ahead", 5)
		}
		return nil
	}
	if version > int(target) {
		return fail("ahead", 5)
	}
	if dirty {
		if version != 12 {
			return fail("dirty", 4)
		}
		if err := resumeLocked(driver, sourceDriver); err != nil {
			return fail("apply_failure", 8)
		}
	}
	start := version + 1
	if start < 1 {
		start = 1
	}
	for next := start; next <= int(target); next++ {
		reader, _, err := sourceDriver.ReadUp(uint(next))
		if err != nil {
			return fail("invalid_source", 9)
		}
		err = driver.SetVersion(next, true)
		if err == nil {
			err = driver.Run(reader)
		}
		_ = reader.Close()
		if err != nil {
			return fail("apply_failure", 8)
		}
		if err := driver.SetVersion(next, false); err != nil {
			return fail("apply_failure", 8)
		}
	}
	return json.NewEncoder(output).Encode(map[string]any{"binary_target": target, "database_version": target, "state": "current", "changed": version != int(target) || dirty})
}
func schemaState(version int, dirty bool, target uint) string {
	if version > int(target) {
		return "ahead"
	}
	if dirty {
		return "dirty"
	}
	if version < 1 {
		return "clean"
	}
	if version < int(target) {
		return "behind"
	}
	return "current"
}
func lockFailure(err error) error {
	if errors.Is(err, database.ErrLocked) || errors.Is(err, migrate.ErrLockTimeout) {
		return fail("lock_timeout", 6)
	}
	return fail("connection_failure", 3)
}
func resumeSuperAdminMigration(driver database.Driver, source source.Driver) (err error) {
	if err = driver.Lock(); err != nil {
		return err
	}
	defer func() {
		if unlockErr := driver.Unlock(); err == nil {
			err = unlockErr
		}
	}()
	version, dirty, err := driver.Version()
	if err != nil {
		return err
	}
	if version != 12 || !dirty {
		return nil
	}
	return resumeLocked(driver, source)
}
func resumeLocked(driver database.Driver, source source.Driver) error {
	reader, _, err := source.ReadUp(12)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err = driver.Run(reader); err != nil {
		return err
	}
	return driver.SetVersion(12, false)
}
