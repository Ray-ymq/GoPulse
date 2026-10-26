package user

import (
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestIsDuplicateEntry(t *testing.T) {
	if !isDuplicateEntry(&mysql.MySQLError{Number: 1062, Message: "duplicate"}) {
		t.Fatal("duplicate entry was not recognized")
	}
	if isDuplicateEntry(&mysql.MySQLError{Number: 1452, Message: "foreign key"}) {
		t.Fatal("non-duplicate MySQL error was recognized as duplicate")
	}
	if isDuplicateEntry(errors.New("duplicate")) {
		t.Fatal("plain error was recognized as duplicate")
	}
}
