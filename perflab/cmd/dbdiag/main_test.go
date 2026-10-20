package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

type fixtureDriver struct{}
type fixtureConn struct{}
type fixtureRows struct{ remaining int }

func (fixtureDriver) Open(string) (driver.Conn, error)  { return fixtureConn{}, nil }
func (fixtureConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (fixtureConn) Begin() (driver.Tx, error)           { return nil, errors.New("unsupported") }
func (fixtureConn) Close() error                        { return nil }
func (fixtureConn) QueryContext(ctx context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(statement, "SLOW"):
		<-ctx.Done()
		return nil, ctx.Err()
	case strings.Contains(statement, "DENIED"):
		return nil, errors.New("fixture-user fixture-password fixture-database denied")
	case strings.Contains(statement, "MANY"):
		return &fixtureRows{201}, nil
	default:
		return &fixtureRows{1}, nil
	}
}
func (*fixtureRows) Columns() []string { return []string{"value", "nullable"} }
func (*fixtureRows) Close() error      { return nil }
func (r *fixtureRows) Next(values []driver.Value) error {
	if r.remaining == 0 {
		return io.EOF
	}
	r.remaining--
	values[0], values[1] = "1", nil
	return nil
}

func init() { sql.Register("pt4-fixture", fixtureDriver{}) }

func TestIndependentTimeoutAndRedaction(t *testing.T) {
	db, err := sql.Open("pt4-fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	cfg := &mysql.Config{User: "fixture-user", Passwd: "fixture-password", DBName: "fixture-database"}
	result := sample(context.Background(), db, []query{{"slow", "SLOW"}, {"fast", "FAST"}, {"denied", "DENIED"}}, 10*time.Millisecond, cfg)
	entries := result["queries"].(map[string]any)
	if entries["slow"].(map[string]any)["ok"] != false || entries["fast"].(map[string]any)["ok"] != true {
		t.Fatal("一项查询超时影响了后续查询")
	}
	message := entries["denied"].(map[string]any)["error"].(string)
	if strings.Contains(message, "fixture-") {
		t.Fatal("采样错误包含未脱敏的凭据")
	}
}

func TestNullableValuesAndTruncation(t *testing.T) {
	db, err := sql.Open("pt4-fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	result := sample(context.Background(), db, []query{{"locks", "MANY"}}, time.Second, &mysql.Config{})
	entry := result["queries"].(map[string]any)["locks"].(map[string]any)
	values := entry["rows"].([]map[string]any)
	if entry["truncated"] != true || len(values) != 200 || values[0]["nullable"] != nil {
		t.Fatal("锁截断标记或 SQL NULL 未保留")
	}
}
