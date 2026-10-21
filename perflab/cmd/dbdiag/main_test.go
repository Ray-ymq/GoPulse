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
	awaitRows(t, ctx, db, query{"row_locks", rowLocksSQL, []any{cfg.DBName, expandedRowLimit + 1}}, func(rows []map[string]any) bool {
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

func TestCapturesMoreThan128RowLockEdges(t *testing.T) {
	db, cfg := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS large_lock_fixture(id INT PRIMARY KEY,value INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "REPLACE INTO large_lock_fixture VALUES(1,0)"); err != nil {
		t.Fatal(err)
	}
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx, "UPDATE large_lock_fixture SET value=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	waitCtx, stop := context.WithCancel(ctx)
	const waiters = 18
	done := make(chan error, waiters)
	defer func() {
		stop()
		_ = holder.Rollback()
		for range waiters {
			<-done
		}
	}()
	for range waiters {
		go func() {
			_, err := db.ExecContext(waitCtx, "UPDATE large_lock_fixture SET value=2 WHERE id=1")
			done <- err
		}()
	}
	awaitRows(t, ctx, db, query{"row_locks", rowLocksSQL, []any{cfg.DBName, expandedRowLimit + 1}}, func(rows []map[string]any) bool {
		return len(rows) > rowLimit && rows[0]["waiting_trx_id"] != nil && rows[0]["blocking_trx_id"] != nil
	})
}

func TestExpandedRowsAndByteBoundsRemainEnforced(t *testing.T) {
	db, _ := fixture(t)
	q := query{"row_locks", `WITH RECURSIVE n AS (SELECT 1 AS id UNION ALL SELECT id+1 FROM n WHERE id<1000)
 SELECT id FROM n CROSS JOIN (SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5) copies`, nil}
	rows, clipped, err := readRows(context.Background(), db, q)
	if err != nil || !clipped || len(rows) != expandedRowLimit {
		t.Fatalf("扩大行数上限后未正确标记截断: rows=%d clipped=%v err=%v", len(rows), clipped, err)
	}
	q = query{"row_locks", "SELECT 1 AS id,'small' AS value UNION ALL SELECT 2,REPEAT('x',1024)", nil}
	rows, clipped, err = readRowsWithin(context.Background(), db, q, 128)
	encoded, _ := json.Marshal(rows)
	if err != nil || !clipped || len(rows) != 1 || len(encoded) > 128 {
		t.Fatalf("字节上限未保留已有行或未标记截断: rows=%d bytes=%d err=%v", len(rows), len(encoded), err)
	}
}

func TestHistoryCollectsNewEventsWithoutRepeatingOldRows(t *testing.T) {
	db, cfg := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cursor := &historyCursor{}
	first := snapshot(ctx, db, cfg.DBName, cfg.User, "baseline", true, cursor)
	if len(first["errors"].([]string)) != 0 || cursor.statements == 0 || cursor.syncs == 0 {
		t.Fatalf("历史基线读取失败: %v", first["errors"])
	}
	const failingSQL = "SELECT missing_incremental_fixture_column FROM business_environment"
	for range 150 {
		if _, err := db.ExecContext(ctx, failingSQL); err == nil {
			t.Fatal("fixture 错误语句意外成功")
		}
	}
	second := snapshot(ctx, db, cfg.DBName, cfg.User, "periodic", true, cursor)
	if len(second["errors"].([]string)) != 0 || len(second["truncated"].([]string)) != 0 {
		t.Fatalf("增量历史读取失败: errors=%v truncated=%v", second["errors"], second["truncated"])
	}
	count := 0
	for _, row := range second["statement_history"].([]map[string]any) {
		if row["SQL_TEXT"] == failingSQL {
			count++
		}
	}
	if count != 150 {
		t.Fatalf("新历史事件保存不完整: got=%d want=150", count)
	}
	third := snapshot(ctx, db, cfg.DBName, cfg.User, "manual", true, cursor)
	if len(third["errors"].([]string)) != 0 || len(third["truncated"].([]string)) != 0 {
		t.Fatalf("重复采样失败: errors=%v truncated=%v", third["errors"], third["truncated"])
	}
	for _, row := range third["statement_history"].([]map[string]any) {
		if row["SQL_TEXT"] == failingSQL {
			t.Fatal("旧历史事件占用了下一次采样窗口")
		}
	}
	seen := map[string]bool{}
	for _, row := range second["sync_history"].([]map[string]any) {
		key, _ := json.Marshal([]any{row["THREAD_ID"], row["EVENT_ID"]})
		seen[string(key)] = true
	}
	for _, row := range third["sync_history"].([]map[string]any) {
		key, _ := json.Marshal([]any{row["THREAD_ID"], row["EVENT_ID"]})
		if seen[string(key)] {
			t.Fatal("磁盘同步历史重复出现在相邻窗口")
		}
	}
	before := *cursor
	failedCtx, fail := context.WithCancel(ctx)
	fail()
	failed := snapshot(failedCtx, db, cfg.DBName, cfg.User, "manual", true, cursor)
	if len(failed["errors"].([]string)) == 0 || *cursor != before {
		t.Fatal("历史查询失败后仍推进了游标")
	}
}

func TestHistoryIncludesStatementsThatFinishAfterBoundary(t *testing.T) {
	db, cfg := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const slowSQL = "SELECT SLEEP(0.5)"
	done := make(chan error, 1)
	go func() {
		_, err := db.ExecContext(ctx, slowSQL)
		done <- err
	}()
	awaitRows(t, ctx, db, query{"fixture", "SELECT TIMER_START FROM performance_schema.events_statements_current WHERE SQL_TEXT=? AND END_EVENT_ID IS NULL", []any{slowSQL}}, func(rows []map[string]any) bool {
		return len(rows) > 0
	})
	cursor := &historyCursor{}
	first := snapshot(ctx, db, cfg.DBName, cfg.User, "baseline", true, cursor)
	boundary := cursor.statements
	if len(first["errors"].([]string)) != 0 || boundary == 0 {
		t.Fatalf("历史基线读取失败: %v", first["errors"])
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	second := snapshot(ctx, db, cfg.DBName, cfg.User, "periodic", true, cursor)
	if len(second["errors"].([]string)) != 0 {
		t.Fatalf("历史增量读取失败: %v", second["errors"])
	}
	found := false
	for _, row := range second["statement_history"].([]map[string]any) {
		found = found || row["SQL_TEXT"] == slowSQL
	}
	var started, ended uint64
	err := db.QueryRowContext(ctx, "SELECT TIMER_START,TIMER_END FROM performance_schema.events_statements_history_long WHERE SQL_TEXT=? ORDER BY TIMER_END DESC LIMIT 1", slowSQL).Scan(&started, &ended)
	if err != nil || !found || started >= boundary || ended <= boundary {
		t.Fatalf("跨窗口语句未按完成时间保存: found=%v start=%d boundary=%d end=%d err=%v", found, started, boundary, ended, err)
	}
}

func TestTruncatedHistoryKeepsCursorAndReportsIncomplete(t *testing.T) {
	db, cfg := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, "TRUNCATE TABLE performance_schema.events_statements_history_long"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.Exec("TRUNCATE TABLE performance_schema.events_statements_history_long") }()
	cursor := &historyCursor{}
	first := snapshot(ctx, db, cfg.DBName, cfg.User, "baseline", true, cursor)
	if len(first["errors"].([]string)) != 0 || cursor.statements == 0 {
		t.Fatalf("历史基线读取失败: %v", first["errors"])
	}
	before := cursor.statements
	for range expandedRowLimit + 1 {
		if _, err := db.ExecContext(ctx, "SELECT missing_truncation_fixture_column FROM business_environment"); err == nil {
			t.Fatal("fixture 错误语句意外成功")
		}
	}
	second := snapshot(ctx, db, cfg.DBName, cfg.User, "periodic", true, cursor)
	clipped := false
	for _, name := range second["truncated"].([]string) {
		clipped = clipped || name == "statement_history"
	}
	windows := second["history_windows"].(map[string]any)
	advanced := windows["statement_history"].(map[string]any)["advanced"]
	encoded, err := json.Marshal(second)
	if err != nil || !clipped || advanced != false || cursor.statements != before || len(encoded) >= 4<<20 {
		t.Fatalf("截断历史的游标、标记或记录大小不符合要求: clipped=%v advanced=%v bytes=%d err=%v", clipped, advanced, len(encoded), err)
	}
}

func TestPeriodicAndTriggeredHistoryShareCursor(t *testing.T) {
	_, cfg := fixture(t)
	path := filepath.Join(t.TempDir(), "diagnostic.dsn")
	if err := os.WriteFile(path, []byte(cfg.FormatDSN()), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1600*time.Millisecond)
	defer cancel()
	var output bytes.Buffer
	if err := run(ctx, options{DSNFile: path, Fast: 100 * time.Millisecond, Summary: time.Second},
		strings.NewReader("manual\nstage_end\n"), &output); err != nil {
		t.Fatal(err)
	}
	last := map[string]float64{}
	decoder := json.NewDecoder(&output)
	count := 0
	for {
		var row map[string]any
		if err := decoder.Decode(&row); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		windows, _ := row["history_windows"].(map[string]any)
		if len(windows) == 0 {
			continue
		}
		count++
		for name, raw := range windows {
			window := raw.(map[string]any)
			if window["begin_timer_exclusive"] != last[name] {
				t.Fatalf("周期与触发采样的历史区间不连续: %s", name)
			}
			if window["advanced"] == true {
				last[name] = window["end_timer_inclusive"].(float64)
			}
		}
	}
	if count < 4 {
		t.Fatalf("周期或触发历史未执行: snapshots=%d", count)
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
