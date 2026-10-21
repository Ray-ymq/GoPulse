package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func fixture(t *testing.T) (*sql.DB, *mysql.Config) {
	t.Helper()
	path := os.Getenv("GOPULSE_DIAGNOSTIC_TEST_DSN_FILE")
	if path == "" {
		t.Skip("未指定独立 MySQL 诊断 fixture")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("无法读取 fixture DSN")
	}
	cfg, err := mysql.ParseDSN(strings.TrimSpace(string(raw)))
	if err != nil || cfg.DBName != "gopulse_diag_fixture" {
		t.Fatal("fixture 必须使用独立 gopulse_diag_fixture 数据库")
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("无法连接 fixture")
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		"UPDATE performance_schema.setup_consumers SET ENABLED='YES'",
		"UPDATE performance_schema.setup_instruments SET ENABLED='YES',TIMED='YES' WHERE NAME LIKE 'statement/%' OR NAME LIKE 'wait/io/file/%' OR NAME='wait/lock/metadata/sql/mdl'",
		"CREATE TABLE IF NOT EXISTS business_environment(id INT PRIMARY KEY)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("fixture 准备失败: %v", err)
		}
	}
	return db, cfg
}

func TestDiagnosticReadOnlyAndAllQueries(t *testing.T) {
	db, cfg := fixture(t)
	for _, q := range []string{
		"CREATE USER IF NOT EXISTS 'diag_reader'@'%' IDENTIFIED BY 'fixture-password' WITH MAX_USER_CONNECTIONS 4",
		"GRANT PROCESS ON *.* TO 'diag_reader'@'%'",
		"GRANT SELECT ON performance_schema.* TO 'diag_reader'@'%'",
		"GRANT SELECT ON gopulse_diag_fixture.business_environment TO 'diag_reader'@'%'",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	reader := *cfg
	reader.User, reader.Passwd = "diag_reader", "fixture-password"
	path := filepath.Join(t.TempDir(), "diagnostic.dsn")
	if err := os.WriteFile(path, []byte(reader.FormatDSN()), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := run(ctx, options{DSNFile: path, Once: true}, strings.NewReader(""), &output); err != nil {
		t.Fatal("只读诊断运行失败")
	}
	decoder := json.NewDecoder(&output)
	var metrics, scene map[string]any
	if err := decoder.Decode(&metrics); err != nil {
		t.Fatal(err)
	}
	if metrics["status"] != float64(200) || !strings.Contains(metrics["body"].(string), `collector_success{collector="file_io"} 1`) {
		t.Fatal("汇总指标缺少文件 I/O 诊断")
	}
	if err := decoder.Decode(&scene); err != nil {
		t.Fatal(err)
	}
	if scene["type"] != "scene" || len(scene["errors"].([]any)) != 0 || len(scene["configuration"].([]any)) < 20 {
		t.Fatalf("现场查询失败或配置未保存: %v", scene["errors"])
	}
	connection, err := sql.Open("mysql", reader.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.Exec("INSERT INTO business_environment VALUES(1)"); err == nil {
		t.Fatal("诊断账号不应具备业务写权限")
	}
}

func TestCapturesRowAndMetadataLocks(t *testing.T) {
	db, cfg := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS lock_fixture(id INT PRIMARY KEY,value INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "REPLACE INTO lock_fixture VALUES(1,0)"); err != nil {
		t.Fatal(err)
	}
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx, "UPDATE lock_fixture SET value=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	waitCtx, stopWait := context.WithCancel(ctx)
	defer stopWait()
	done := make(chan error, 1)
	go func() {
		_, err := db.ExecContext(waitCtx, "UPDATE lock_fixture SET value=2 WHERE id=1")
		done <- err
	}()
	awaitRows(t, ctx, db, query{"row_locks", rowLocksSQL, []any{cfg.DBName}}, func(rows []map[string]any) bool {
		return len(rows) > 0 && rows[0]["waiting_trx_id"] != nil && rows[0]["blocking_trx_id"] != nil && rows[0]["OBJECT_NAME"] == "lock_fixture"
	})
	stopWait()
	<-done
	_ = holder.Rollback()

	holder, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	var value int
	if err := holder.QueryRowContext(ctx, "SELECT value FROM lock_fixture WHERE id=1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	waitCtx, stopWait = context.WithCancel(ctx)
	defer stopWait()
	go func() {
		_, err := db.ExecContext(waitCtx, "ALTER TABLE lock_fixture COMMENT='metadata fixture'")
		done <- err
	}()
	awaitRows(t, ctx, db, query{"metadata_locks", metadataLocksSQL, []any{cfg.DBName}}, func(rows []map[string]any) bool {
		pending, granted := false, false
		for _, row := range rows {
			pending = pending || row["LOCK_STATUS"] == "PENDING"
			granted = granted || row["LOCK_STATUS"] == "GRANTED"
		}
		return pending && granted
	})
	stopWait()
	_ = holder.Rollback()
	<-done
}

func awaitRows(t *testing.T, ctx context.Context, db *sql.DB, q query, accept func([]map[string]any) bool) {
	t.Helper()
	for ctx.Err() == nil {
		rows, _, err := readRows(ctx, db, q)
		if err != nil {
			t.Fatalf("现场查询 %s 失败: %v", q.name, err)
		}
		if accept(rows) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("没有捕获预期的锁等待")
}

func TestReadRowsLimitAndNull(t *testing.T) {
	db, _ := fixture(t)
	rows, truncated, err := readRows(context.Background(), db, query{"fixture", "WITH RECURSIVE n AS (SELECT 1 AS id UNION ALL SELECT id+1 FROM n WHERE id<129) SELECT id,NULL AS nullable FROM n", nil})
	if err != nil || !truncated || len(rows) != rowLimit || rows[0]["nullable"] != nil {
		t.Fatalf("有界输出或 NULL 处理失败: %v", err)
	}
}

func TestInvalidDSNIsNotPrinted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostic.dsn")
	if err := os.WriteFile(path, []byte("secret-password@invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), options{DSNFile: path, Once: true}, strings.NewReader(""), io.Discard); err == nil {
		t.Fatal("无效 DSN 未被拒绝")
	}
}
