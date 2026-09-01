package like

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
)

type fakeDatabase struct {
	exec func(context.Context, string, ...any) (sql.Result, error)
}

func (database *fakeDatabase) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	return database.exec(ctx, query, arguments...)
}

func (database *fakeDatabase) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

func TestRepositoryCreateMapsOnlyDuplicateEntry(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "success"},
		{name: "duplicate", err: &mysql.MySQLError{Number: 1062, Message: "duplicate"}, want: ErrAlreadyExists},
		{name: "wrapped duplicate", err: fmt.Errorf("driver: %w", &mysql.MySQLError{Number: 1062, Message: "duplicate"}), want: ErrAlreadyExists},
		{name: "foreign key", err: &mysql.MySQLError{Number: 1452, Message: "foreign key"}},
		{name: "connection", err: errors.New("connection lost")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := NewMySQLRepository(&fakeDatabase{exec: func(_ context.Context, query string, arguments ...any) (sql.Result, error) {
				if query != `INSERT INTO post_likes (post_id, user_id) VALUES (?, ?)` || len(arguments) != 2 || arguments[0] != uint64(31) || arguments[1] != uint64(17) {
					t.Fatalf("ExecContext() query=%q arguments=%#v", query, arguments)
				}
				return driver.RowsAffected(1), test.err
			}})
			err := repository.Create(context.Background(), 31, 17)
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("Create() error=%v, want %v", err, test.want)
				}
				return
			}
			if test.err == nil && err != nil {
				t.Fatalf("Create() error=%v", err)
			}
			if test.err != nil && (err == nil || errors.Is(err, ErrAlreadyExists) || !errors.Is(err, test.err)) {
				t.Fatalf("Create() error=%v, want preserved non-duplicate failure", err)
			}
		})
	}
}

func TestRepositoryDeleteIsIdempotentAndPreservesErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		rows driver.RowsAffected
		err  error
	}{
		{name: "deleted", rows: 1},
		{name: "already absent", rows: 0},
		{name: "database failure", err: errors.New("delete failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := NewMySQLRepository(&fakeDatabase{exec: func(context.Context, string, ...any) (sql.Result, error) {
				return test.rows, test.err
			}})
			err := repository.Delete(context.Background(), 31, 17)
			if test.err == nil && err != nil {
				t.Fatalf("Delete() error=%v", err)
			}
			if test.err != nil && !errors.Is(err, test.err) {
				t.Fatalf("Delete() error=%v, want preserved failure", err)
			}
		})
	}
}
