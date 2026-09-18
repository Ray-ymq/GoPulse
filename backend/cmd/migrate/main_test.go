package main

import (
	"bytes"
	"errors"
	migrationfiles "github.com/Ray-ymq/GoPulse/backend/migrations"
	"github.com/golang-migrate/migrate/v4/database"
	"io"
	"strings"
	"testing"
)

func TestRunRejectsInvalidArgumentsBeforeLoadingConfiguration(t *testing.T) {
	for _, args := range [][]string{nil, {"up", "extra"}, {"invalid"}} {
		err := run(args, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "usage: migrate <validate|status|up|down>") {
			t.Fatalf("run(%v) error = %v, want usage error", args, err)
		}
	}
}

func TestSchemaStates(t *testing.T) {
	for _, tc := range []struct {
		version int
		dirty   bool
		want    string
	}{
		{-1, false, "clean"}, {12, false, "behind"}, {13, false, "current"}, {11, true, "dirty"}, {14, false, "ahead"}, {14, true, "ahead"},
	} {
		if got := schemaState(tc.version, tc.dirty, 13); got != tc.want {
			t.Fatalf("state = %s, want %s", got, tc.want)
		}
	}
}
func TestValidateDoesNotRequireDatabase(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"validate"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"binary_target":13`) {
		t.Fatal(output.String())
	}
}

type resumeDriver struct {
	database.Driver
	version           int
	dirty             bool
	runs, sets, locks int
	runErr            error
}

func (d *resumeDriver) Lock() error                 { d.locks++; return nil }
func (d *resumeDriver) Unlock() error               { d.locks--; return nil }
func (d *resumeDriver) Version() (int, bool, error) { return d.version, d.dirty, nil }
func (d *resumeDriver) Run(io.Reader) error         { d.runs++; return d.runErr }
func (d *resumeDriver) SetVersion(v int, dirty bool) error {
	d.version, d.dirty = v, dirty
	d.sets++
	return nil
}
func TestResumeOnlyExplicitDirtyTwelve(t *testing.T) {
	for _, tc := range []struct {
		version       int
		dirty, failed bool
		runs, sets    int
	}{
		{12, true, false, 1, 1}, {12, true, true, 1, 0}, {12, false, false, 0, 0}, {11, true, false, 0, 0}, {13, true, false, 0, 0},
	} {
		source, err := migrationfiles.Source()
		if err != nil {
			t.Fatal(err)
		}
		driver := &resumeDriver{version: tc.version, dirty: tc.dirty}
		if tc.failed {
			driver.runErr = errors.New("SQL secret must remain private")
		}
		err = resumeSuperAdminMigration(driver, source)
		source.Close()
		if (err != nil) != tc.failed || driver.runs != tc.runs || driver.sets != tc.sets || driver.locks != 0 {
			t.Fatalf("case=%+v driver=%+v err=%v", tc, driver, err)
		}
		if tc.failed && !driver.dirty {
			t.Fatal("failed resume cleared dirty")
		}
	}
}
