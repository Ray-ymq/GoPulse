package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
)

func main() {
	detailsOnly := flag.Bool("details", false, "每 5 秒采集数据库状态")
	file := flag.String("dsn-file", "", "私有 DSN 文件")
	flag.Parse()
	raw, err := os.ReadFile(*file)
	if err != nil {
		os.Exit(2)
	}
	cfg, err := mysql.ParseDSN(strings.TrimSpace(string(raw)))
	if err != nil {
		os.Exit(2)
	}
	cfg.Timeout = time.Second
	cfg.ReadTimeout = time.Second
	cfg.WriteTimeout = time.Second
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	encoder := json.NewEncoder(os.Stdout)
	interval := time.Second
	if *detailsOnly {
		interval = 5 * time.Second
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if *detailsOnly {
			if encoder.Encode(sampleState(ctx, cfg)) != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			continue
		}
		start := time.Now()
		row := map[string]any{"timestamp": start.UTC(), "ok": false, "phase": "connect"}
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		db, err := sql.Open("mysql", cfg.FormatDSN())
		if err == nil {
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(0)
			var connection *sql.Conn
			connection, err = db.Conn(probeCtx)
			if err == nil {
				row["phase"] = "select1"
				var one int
				err = connection.QueryRowContext(probeCtx, "SELECT 1").Scan(&one)
				if err == nil && one == 1 {
					row["ok"] = true
				}
				_ = connection.Close()
			}
			_ = db.Close()
		}
		cancel()
		row["elapsed_ms"] = float64(time.Since(start).Microseconds()) / 1000
		if err != nil {
			row["error"] = redactError(err, cfg)
		}
		if encoder.Encode(row) != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func sampleState(ctx context.Context, cfg *mysql.Config) map[string]any {
	details := map[string]any{"timestamp": time.Now().UTC(), "phase": "database_state"}
	detailsCtx, detailsCancel := context.WithTimeout(ctx, 2*time.Second)
	stateDB, openErr := sql.Open("mysql", cfg.FormatDSN())
	if openErr == nil {
		stateDB.SetMaxOpenConns(1)
		stateDB.SetMaxIdleConns(0)
		queries := map[string]string{
			"status":       "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running','Uptime','Connections','Aborted_clients','Aborted_connects','Innodb_row_lock_waits','Innodb_row_lock_time')",
			"processlist":  "SELECT ID, COMMAND, TIME, STATE FROM information_schema.PROCESSLIST",
			"transactions": "SELECT trx_state,trx_started,trx_wait_started,trx_mysql_thread_id,trx_rows_locked,trx_rows_modified FROM information_schema.INNODB_TRX",
			"locks":        "SELECT OBJECT_SCHEMA,OBJECT_NAME,LOCK_TYPE,LOCK_MODE,LOCK_STATUS FROM performance_schema.data_locks LIMIT 200",
			"waits":        "SELECT EVENT_NAME,COUNT_STAR,SUM_TIMER_WAIT FROM performance_schema.events_waits_summary_global_by_event_name WHERE COUNT_STAR>0 ORDER BY SUM_TIMER_WAIT DESC LIMIT 30",
			"environment":  "SELECT data_epoch,status,mode FROM business_environment WHERE singleton=1",
			"migration":    "SELECT version,dirty FROM schema_migrations",
			"backlog":      "SELECT open_requirements,retained_groups,retained_bytes,parked_messages FROM business_async_budget",
		}
		for name, query := range queries {
			values, queryErr := queryRows(detailsCtx, stateDB, query)
			if queryErr != nil {
				details[name+"_error"] = redactError(queryErr, cfg)
			} else {
				details[name] = values
			}
		}
		_ = stateDB.Close()
	} else {
		details["error"] = redactError(openErr, cfg)
	}
	detailsCancel()
	return details
}

func queryRows(ctx context.Context, db *sql.DB, query string) ([]map[string]string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []map[string]string{}
	for rows.Next() {
		values := make([]sql.RawBytes, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := map[string]string{}
		for i, name := range columns {
			row[name] = string(values[i])
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func redactError(err error, cfg *mysql.Config) string {
	message := err.Error()
	for _, secret := range []string{cfg.Passwd, cfg.User, cfg.DBName} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}
