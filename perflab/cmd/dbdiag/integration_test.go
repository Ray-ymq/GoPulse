package main

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestIntegrationControlledLocksAndDigest(t *testing.T) {
	file := os.Getenv("PT4_FIXTURE_DSN_FILE")
	if file == "" {
		t.Skip("仅通过测试四独立数据库 fixture 执行")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal("无法读取独立测试库配置")
	}
	cfg, err := mysql.ParseDSN(strings.TrimSpace(string(raw)))
	if err != nil || cfg.DBName != "pt4_fixture" {
		t.Fatal("拒绝连接非 fixture 数据库")
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("独立测试库连接失败")
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, statement := range []string{"CREATE TABLE fixture_locks(id INT PRIMARY KEY, value INT)", "INSERT INTO fixture_locks VALUES (1,0),(2,0)"} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	before := sample(ctx, db, queries["digest"], time.Second, cfg)
	for range 5 {
		var value int
		if err := db.QueryRowContext(ctx, "SELECT value FROM fixture_locks WHERE id=2").Scan(&value); err != nil {
			t.Fatal(err)
		}
	}
	after := sample(ctx, db, queries["digest"], time.Second, cfg)
	if before["ok"] != true || after["ok"] != true {
		t.Fatal("SQL 摘要采样失败")
	}
	found := false
	for _, row := range after["queries"].(map[string]any)["digests"].(map[string]any)["rows"].([]map[string]any) {
		text, _ := row["DIGEST_TEXT"].(string)
		if strings.HasPrefix(text, "SELECT ") && strings.Contains(text, "fixture_locks") && row["COUNT_STAR"] == "5" {
			found = true
		}
	}
	if !found {
		t.Fatal("未找到恰好执行 5 次的 SQL 摘要")
	}
	holder, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()
	waiter, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = waiter.Close() }()
	var holderID, waiterID string
	if err := holder.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&holderID); err != nil {
		t.Fatal(err)
	}
	if err := waiter.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&waiterID); err != nil {
		t.Fatal(err)
	}
	tx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "UPDATE fixture_locks SET value=value+1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	waitCtx, waitCancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = waiter.ExecContext(waitCtx, "UPDATE fixture_locks SET value=value+1 WHERE id=1")
	}()
	defer func() { waitCancel(); <-done }()
	for range 30 {
		result := sample(ctx, db, queries["locks"][:1], time.Second, cfg)
		entry := result["queries"].(map[string]any)["locks"].(map[string]any)
		if entry["ok"] == true {
			for _, row := range entry["rows"].([]map[string]any) {
				if row["waiting_connection_id"] == waiterID && row["blocking_connection_id"] == holderID && row["OBJECT_NAME"] == "fixture_locks" && row["LOCK_STATUS"] == "WAITING" {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("阻塞链未映射到预期的等待者、持锁者和测试表")
}
