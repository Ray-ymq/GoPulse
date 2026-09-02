package notification

import (
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestIsDuplicateEntry(t *testing.T) {
	if !isDuplicateEntry(&mysql.MySQLError{Number: 1062}) {
		t.Fatal("duplicate entry was not recognized")
	}
	if isDuplicateEntry(&mysql.MySQLError{Number: 1452}) || isDuplicateEntry(errors.New("duplicate")) {
		t.Fatal("non-duplicate error was recognized")
	}
}
