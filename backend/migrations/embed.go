package migrations

import (
	"embed"

	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// files contains every versioned SQL migration shipped with the Backend.
//
//go:embed *.sql
var files embed.FS

// Source returns a fresh migration source backed by the embedded SQL files.
func Source() (source.Driver, error) {
	return iofs.New(files, ".")
}
