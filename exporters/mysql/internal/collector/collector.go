// Package collector maps only server status; it never accepts SQL from callers.
package collector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const StatusQuery = "SHOW GLOBAL STATUS WHERE Variable_name IN ('Uptime','Threads_connected','Threads_running','Queries','Slow_queries','Com_commit','Com_rollback','Innodb_buffer_pool_bytes_data','Innodb_buffer_pool_bytes_dirty','Connections','Aborted_connects','Aborted_clients','Threads_created','Max_used_connections','Com_select','Com_insert','Com_update','Com_delete','Innodb_buffer_pool_read_requests','Innodb_buffer_pool_reads','Innodb_buffer_pool_pages_total','Innodb_buffer_pool_pages_free','Innodb_row_lock_waits','Innodb_row_lock_time','Innodb_row_lock_current_waits','Created_tmp_tables','Created_tmp_disk_tables','Innodb_data_read','Innodb_data_written','Innodb_data_pending_reads','Innodb_data_pending_writes','Innodb_os_log_pending_fsyncs','Innodb_os_log_pending_writes','Innodb_log_waits')"

type field struct{ name, upstream, kind, label string }

var fields = []field{
	{"uptime_seconds", "Uptime", "gauge", ""},
	{"connections", "Threads_connected", "gauge", ""},
	{"max_connections", "max_connections", "gauge", ""},
	{"threads_running", "Threads_running", "gauge", ""},
	{"queries_total", "Queries", "counter", ""},
	{"slow_queries_total", "Slow_queries", "counter", ""},
	{"transactions_total", "Com_commit", "counter", `{result="commit"}`},
	{"transactions_total", "Com_rollback", "counter", `{result="rollback"}`},
	{"buffer_pool_data_bytes", "Innodb_buffer_pool_bytes_data", "gauge", ""},
	{"buffer_pool_dirty_bytes", "Innodb_buffer_pool_bytes_dirty", "gauge", ""},
	{"connection_attempts_total", "Connections", "counter", ""},
	{"aborted_connections_total", "Aborted_connects", "counter", ""},
	{"aborted_clients_total", "Aborted_clients", "counter", ""},
	{"threads_created_total", "Threads_created", "counter", ""},
	{"connections_peak", "Max_used_connections", "gauge", ""},
	{"statements_total", "Com_select", "counter", `{operation="select"}`},
	{"statements_total", "Com_insert", "counter", `{operation="insert"}`},
	{"statements_total", "Com_update", "counter", `{operation="update"}`},
	{"statements_total", "Com_delete", "counter", `{operation="delete"}`},
	{"buffer_pool_read_requests_total", "Innodb_buffer_pool_read_requests", "counter", ""},
	{"buffer_pool_reads_total", "Innodb_buffer_pool_reads", "counter", ""},
	{"buffer_pool_pages", "Innodb_buffer_pool_pages_total", "gauge", `{state="total"}`},
	{"buffer_pool_pages", "Innodb_buffer_pool_pages_free", "gauge", `{state="free"}`},
	{"row_lock_waits_total", "Innodb_row_lock_waits", "counter", ""},
	{"row_lock_wait_seconds_total", "Innodb_row_lock_time", "counter", ""},
	{"row_lock_current_waits", "Innodb_row_lock_current_waits", "gauge", ""},
	{"temporary_tables_total", "Created_tmp_tables", "counter", ""},
	{"temporary_disk_tables_total", "Created_tmp_disk_tables", "counter", ""},
	{"data_read_bytes_total", "Innodb_data_read", "counter", ""},
	{"data_written_bytes_total", "Innodb_data_written", "counter", ""},
	{"data_pending", "Innodb_data_pending_reads", "gauge", `{operation="read"}`},
	{"data_pending", "Innodb_data_pending_writes", "gauge", `{operation="write"}`},
	{"log_pending", "Innodb_os_log_pending_fsyncs", "gauge", `{operation="fsync"}`},
	{"log_pending", "Innodb_os_log_pending_writes", "gauge", `{operation="write"}`},
	{"redo_log_waits_total", "Innodb_log_waits", "counter", ""},
}

var fieldScale = map[string]float64{"Innodb_row_lock_time": 0.001}

var ErrUnavailable = errors.New("target_unavailable")

const Unavailable = "# TYPE gopulse_mysql_up gauge\ngopulse_mysql_up 0\n"

func Collect(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, StatusQuery)
	if err != nil {
		return "", ErrUnavailable
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var name, value string
		if rows.Scan(&name, &value) != nil {
			return "", ErrUnavailable
		}
		if _, ok := values[name]; ok {
			return "", ErrUnavailable
		}
		values[name] = value
	}
	if rows.Err() != nil {
		return "", ErrUnavailable
	}
	var maximum string
	if db.QueryRowContext(ctx, "SELECT @@GLOBAL.max_connections").Scan(&maximum) != nil {
		return "", ErrUnavailable
	}
	values["max_connections"] = maximum
	return Render(values)
}

// Render is all-or-nothing: missing, malformed or negative values never become
// zeros and no previous snapshot is retained.
func Render(values map[string]string) (string, error) {
	var b strings.Builder
	b.WriteString("# TYPE gopulse_mysql_up gauge\ngopulse_mysql_up 1\n")
	seen := map[string]bool{}
	for _, f := range fields {
		value, ok := values[f.upstream]
		if !ok {
			return "", ErrUnavailable
		}
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return "", ErrUnavailable
		}
		name := "gopulse_mysql_" + f.name
		if !seen[name] {
			fmt.Fprintf(&b, "# TYPE %s %s\n", name, f.kind)
			seen[name] = true
		}
		scale, ok := fieldScale[f.upstream]
		if !ok {
			scale = 1
		}
		metricValue := float64(n) * scale
		fmt.Fprintf(&b, "%s%s %s\n", name, f.label, strconv.FormatFloat(metricValue, 'g', -1, 64))
	}
	return b.String(), nil
}
