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

const StatusQuery = "SHOW GLOBAL STATUS WHERE Variable_name IN ('Uptime','Threads_connected','Threads_running','Queries','Slow_queries','Com_commit','Com_rollback','Innodb_buffer_pool_bytes_data','Innodb_buffer_pool_bytes_dirty')"

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
}

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
		fmt.Fprintf(&b, "%s%s %d\n", name, f.label, n)
	}
	return b.String(), nil
}
