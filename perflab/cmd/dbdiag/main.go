package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
)

type query struct {
	name string
	sql  string
}

var queries = map[string][]query{
	"critical": {
		{"status", "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running','Uptime','Connections','Innodb_buffer_pool_read_requests','Innodb_buffer_pool_reads','Innodb_buffer_pool_wait_free','Innodb_buffer_pool_pages_dirty','Innodb_row_lock_waits','Innodb_row_lock_time','Performance_schema_digest_lost')"},
		{"identity", "SELECT @@server_uuid AS server_uuid, @@version AS version"},
		{"environment", "SELECT data_epoch,status,mode FROM business_environment WHERE singleton=1"},
		{"migration", "SELECT version,dirty FROM schema_migrations"},
		{"backlog", "SELECT open_requirements,retained_groups,retained_bytes,parked_messages FROM business_async_budget"},
	},
	"digest": {
		{"identity", "SELECT @@server_uuid AS server_uuid,UNIX_TIMESTAMP() AS server_epoch, VARIABLE_VALUE AS uptime FROM performance_schema.global_status WHERE VARIABLE_NAME='Uptime'"},
		{"lost", "SHOW GLOBAL STATUS LIKE 'Performance_schema_digest_lost'"},
		{"digests", `SELECT SCHEMA_NAME,DIGEST,DIGEST_TEXT,COUNT_STAR,SUM_TIMER_WAIT,SUM_LOCK_TIME,SUM_ROWS_EXAMINED,SUM_ROWS_SENT,SUM_CREATED_TMP_TABLES,SUM_CREATED_TMP_DISK_TABLES,SUM_SORT_ROWS,SUM_ERRORS,FIRST_SEEN,LAST_SEEN FROM performance_schema.events_statements_summary_by_digest WHERE SCHEMA_NAME=DATABASE() OR DIGEST IS NULL`},
	},
	"locks": {
		{"locks", `SELECT w.REQUESTING_THREAD_ID AS waiting_thread_id,rt.PROCESSLIST_ID AS waiting_connection_id,w.BLOCKING_THREAD_ID AS blocking_thread_id,bt.PROCESSLIST_ID AS blocking_connection_id,rl.OBJECT_SCHEMA,rl.OBJECT_NAME,rl.INDEX_NAME,rl.LOCK_TYPE,rl.LOCK_MODE,rl.LOCK_STATUS,bl.LOCK_MODE AS blocking_lock_mode,wt.trx_started AS waiting_trx_started,wt.trx_wait_started AS waiting_since,blt.trx_started AS blocking_trx_started,blt.trx_rows_locked AS blocking_rows_locked,blt.trx_rows_modified AS blocking_rows_modified,rs.DIGEST AS waiting_digest,bs.DIGEST AS blocking_current_digest FROM performance_schema.data_lock_waits w LEFT JOIN performance_schema.threads rt ON rt.THREAD_ID=w.REQUESTING_THREAD_ID LEFT JOIN performance_schema.threads bt ON bt.THREAD_ID=w.BLOCKING_THREAD_ID LEFT JOIN performance_schema.data_locks rl ON rl.ENGINE=w.ENGINE AND rl.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID LEFT JOIN performance_schema.data_locks bl ON bl.ENGINE=w.ENGINE AND bl.ENGINE_LOCK_ID=w.BLOCKING_ENGINE_LOCK_ID LEFT JOIN information_schema.INNODB_TRX wt ON wt.trx_mysql_thread_id=rt.PROCESSLIST_ID LEFT JOIN information_schema.INNODB_TRX blt ON blt.trx_mysql_thread_id=bt.PROCESSLIST_ID LEFT JOIN performance_schema.events_statements_current rs ON rs.THREAD_ID=w.REQUESTING_THREAD_ID LEFT JOIN performance_schema.events_statements_current bs ON bs.THREAD_ID=w.BLOCKING_THREAD_ID LIMIT 201`},
		{"activity", `SELECT t.THREAD_ID,t.PROCESSLIST_ID,t.PROCESSLIST_USER,t.PROCESSLIST_COMMAND,t.PROCESSLIST_TIME,t.PROCESSLIST_STATE,s.DIGEST,s.DIGEST_TEXT FROM performance_schema.threads t LEFT JOIN performance_schema.events_statements_current s ON s.THREAD_ID=t.THREAD_ID WHERE t.TYPE='FOREGROUND' LIMIT 201`},
		{"attributes", "SELECT PROCESSLIST_ID,ATTR_NAME,ATTR_VALUE FROM performance_schema.session_connect_attrs WHERE ATTR_NAME IN ('program_name','_client_name','_pid','role','gopulse_role') LIMIT 201"},
	},
}

func main() {
	file := flag.String("dsn-file", "", "私有 DSN 文件")
	stream := flag.String("stream", "critical", "采样类型：critical、digest 或 locks")
	interval := flag.Duration("interval", 0, "采样间隔；默认 critical/locks 为 5s，digest 为 10s")
	timeout := flag.Duration("timeout", 2*time.Second, "每项查询的独立超时")
	once := flag.Bool("once", false, "只采集一个周期")
	runID := flag.String("run-id", "manual", "证据关联标识")
	flag.Parse()
	list, ok := queries[*stream]
	if !ok || *file == "" || *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "采样参数无效")
		os.Exit(2)
	}
	if *interval == 0 {
		*interval = 5 * time.Second
		if *stream == "digest" {
			*interval = 10 * time.Second
		}
	}
	if *interval <= 0 {
		os.Exit(2)
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法读取私有 DSN 文件")
		os.Exit(2)
	}
	cfg, err := mysql.ParseDSN(strings.TrimSpace(string(raw)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "私有 DSN 格式无效")
		os.Exit(2)
	}
	cfg.Timeout = time.Second
	cfg.ReadTimeout = *timeout + time.Second
	cfg.WriteTimeout = time.Second
	cfg.ConnectionAttributes = "program_name:pt4-observer"
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		os.Exit(2)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	trigger := make(chan os.Signal, 1)
	signal.Notify(trigger, syscall.SIGUSR1)
	defer signal.Stop(trigger)
	encoder := json.NewEncoder(os.Stdout)
	due := time.Now()
	for {
		start := time.Now()
		row := sample(ctx, db, list, *timeout, cfg)
		row["timestamp"] = start.UTC()
		row["ended_at"] = time.Now().UTC()
		row["elapsed_ms"] = float64(time.Since(start).Microseconds()) / 1000
		row["stream"] = *stream
		row["run_id"] = *runID
		due = due.Add(*interval)
		skipped := 0
		for due.Before(time.Now()) {
			due = due.Add(*interval)
			skipped++
		}
		row["skipped_cycles"] = skipped
		if encoder.Encode(row) != nil || *once {
			return
		}
		timer := time.NewTimer(time.Until(due))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-trigger:
			timer.Stop()
			due = time.Now()
		}
	}
}

func sample(ctx context.Context, db *sql.DB, list []query, timeout time.Duration, cfg *mysql.Config) map[string]any {
	result := map[string]any{"ok": true, "queries": map[string]any{}}
	for _, q := range list {
		start := time.Now()
		queryCtx, cancel := context.WithTimeout(ctx, timeout)
		values, err := readRows(queryCtx, db, "/* pt4-observer */ "+q.sql)
		cancel()
		entry := map[string]any{"started_at": start.UTC(), "ended_at": time.Now().UTC(), "elapsed_ms": float64(time.Since(start).Microseconds()) / 1000, "ok": err == nil}
		if err != nil {
			message := err.Error()
			for _, secret := range []string{cfg.Passwd, cfg.User, cfg.DBName} {
				if secret != "" {
					message = strings.ReplaceAll(message, secret, "[redacted]")
				}
			}
			entry["error"] = message
			result["ok"] = false
		} else {
			if (q.name == "locks" || q.name == "activity" || q.name == "attributes") && len(values) > 200 {
				entry["truncated"] = true
				values = values[:200]
			} else {
				entry["truncated"] = false
			}
			entry["rows"] = values
		}
		result["queries"].(map[string]any)[q.name] = entry
	}
	return result
}

func readRows(ctx context.Context, db *sql.DB, statement string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for rows.Next() {
		values := make([]sql.RawBytes, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, name := range columns {
			if values[i] == nil {
				row[name] = nil
			} else {
				row[name] = string(values[i])
			}
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
