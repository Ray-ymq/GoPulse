package main

import (
	"context"
	"database/sql"
	"net"
	"strconv"
	"time"
)

const rowLimit = 128

type query struct {
	name string
	sql  string
	args []any
}

func splitAddress(address string) (string, int, error) {
	host, raw, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(raw)
	return host, port, err
}

func snapshot(ctx context.Context, db *sql.DB, schema, observer, trigger string, history bool) map[string]any {
	start := time.Now()
	row := map[string]any{"type": "scene", "timestamp": start.UTC(), "epoch": float64(start.UnixNano()) / 1e9,
		"trigger": trigger, "history_included": history, "row_limit": rowLimit}
	queries := []query{
		{"transactions", transactionsSQL, []any{observer}},
		{"row_locks", rowLocksSQL, []any{schema}},
		{"metadata_locks", metadataLocksSQL, []any{schema}},
		{"statements", statementsSQL, []any{observer}},
		{"current_waits", currentWaitsSQL, nil},
	}
	if history {
		queries = append(queries, query{"statement_history", statementHistorySQL, []any{schema}},
			query{"prepared_statements", preparedSQL, []any{observer}},
			query{"sync_history", syncHistorySQL, nil})
	}
	if trigger == "baseline" || trigger != "periodic" {
		queries = append(queries, query{"configuration", configurationSQL, nil}, query{"identity", identitySQL, nil},
			query{"digests", digestsSQL, []any{schema}})
	}
	errors, truncated := []string{}, []string{}
	for _, q := range queries {
		queryCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		values, clipped, err := readRows(queryCtx, db, q)
		cancel()
		if err != nil {
			errors = append(errors, q.name)
			continue
		}
		row[q.name] = values
		if clipped {
			truncated = append(truncated, q.name)
		}
	}
	row["errors"], row["truncated"] = errors, truncated
	row["finished_at"], row["elapsed_seconds"] = time.Now().UTC(), time.Since(start).Seconds()
	return row
}

func readRows(ctx context.Context, db *sql.DB, q query) ([]map[string]any, bool, error) {
	limit := rowLimit
	if q.name == "configuration" {
		limit = 1024
	}
	rows, err := db.QueryContext(ctx, q.sql, q.args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	names, err := rows.Columns()
	if err != nil {
		return nil, false, err
	}
	result := []map[string]any{}
	for rows.Next() {
		if len(result) == limit {
			return result, true, nil
		}
		values, destinations := make([]any, len(names)), make([]any, len(names))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, false, err
		}
		item := map[string]any{}
		for i, name := range names {
			value := values[i]
			if bytes, ok := value.([]byte); ok {
				value = string(bytes)
			}
			item[name] = value
		}
		result = append(result, item)
	}
	return result, false, rows.Err()
}
