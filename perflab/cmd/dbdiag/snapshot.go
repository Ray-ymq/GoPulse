package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"time"
)

const rowLimit = 128
const expandedRowLimit = 4096
const sceneDataByteLimit = 3 << 20

func queryRowLimit(name string) int {
	switch name {
	case "row_locks", "statement_history", "sync_history":
		return expandedRowLimit
	case "configuration":
		return 1024
	default:
		return rowLimit
	}
}

type historyCursor struct {
	statements uint64
	syncs      uint64
}

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

func snapshot(ctx context.Context, db *sql.DB, schema, observer, trigger string, history bool, cursor *historyCursor) map[string]any {
	start := time.Now()
	row := map[string]any{"type": "scene", "timestamp": start.UTC(), "epoch": float64(start.UnixNano()) / 1e9,
		"trigger": trigger, "history_included": history, "row_limit": rowLimit,
		"row_limits": map[string]int{"row_locks": expandedRowLimit, "statement_history": expandedRowLimit,
			"sync_history": expandedRowLimit, "configuration": 1024}, "data_byte_limit": sceneDataByteLimit}
	errors, truncated := []string{}, []string{}
	var cutoff, syncCutoff uint64
	syncHistory := history
	if history {
		clockCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		err := db.QueryRowContext(clockCtx, historyClockSQL).Scan(&cutoff)
		cancel()
		if err != nil || cutoff == 0 || cutoff < cursor.statements {
			errors = append(errors, "history_clock")
			history = false
		}
		clockCtx, cancel = context.WithTimeout(ctx, 500*time.Millisecond)
		err = db.QueryRowContext(clockCtx, syncHistoryClockSQL).Scan(&syncCutoff)
		cancel()
		if err != nil {
			errors = append(errors, "sync_history_clock")
			syncHistory = false
		}
		// 缓冲区中的旧事件可能已被覆盖；空区间不把游标退回较小值。
		syncCutoff = max(syncCutoff, cursor.syncs)
	}
	queries := []query{
		{"transactions", transactionsSQL, []any{observer}},
		{"row_locks", rowLocksSQL, []any{schema, expandedRowLimit + 1}},
		{"metadata_locks", metadataLocksSQL, []any{schema}},
		{"statements", statementsSQL, []any{observer}},
		{"current_waits", currentWaitsSQL, nil},
	}
	if history {
		queries = append(queries, query{"statement_history", statementHistorySQL,
			[]any{schema, cursor.statements, cutoff, expandedRowLimit + 1}},
			query{"prepared_statements", preparedSQL, []any{observer}})
	}
	if syncHistory {
		queries = append(queries, query{"sync_history", syncHistorySQL, []any{cursor.syncs, syncCutoff, expandedRowLimit + 1}})
	}
	if trigger == "baseline" || trigger != "periodic" {
		queries = append(queries, query{"configuration", configurationSQL, nil}, query{"identity", identitySQL, nil},
			query{"digests", digestsSQL, []any{schema}})
	}
	remaining := sceneDataByteLimit
	windows := map[string]any{}
	for _, q := range queries {
		queryCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		values, clipped, err := readRowsWithin(queryCtx, db, q, remaining)
		cancel()
		var since *uint64
		end, domain := cutoff, "statement"
		switch q.name {
		case "statement_history":
			since = &cursor.statements
		case "sync_history":
			since = &cursor.syncs
			end, domain = syncCutoff, "wait"
		}
		if since != nil {
			windows[q.name] = map[string]any{"begin_timer_exclusive": *since, "end_timer_inclusive": end, "timer_domain": domain,
				"advanced": err == nil && !clipped}
			// 查询失败或截断时保留游标，避免把未保存的事件跳过。
			if err == nil && !clipped {
				*since = end
			}
		}
		if err != nil {
			errors = append(errors, q.name)
			continue
		}
		row[q.name] = values
		encoded, _ := json.Marshal(values)
		remaining -= len(encoded)
		if clipped {
			truncated = append(truncated, q.name)
		}
	}
	row["history_windows"] = windows
	row["errors"], row["truncated"] = errors, truncated
	row["finished_at"], row["elapsed_seconds"] = time.Now().UTC(), time.Since(start).Seconds()
	return row
}

func readRows(ctx context.Context, db *sql.DB, q query) ([]map[string]any, bool, error) {
	return readRowsWithin(ctx, db, q, sceneDataByteLimit)
}

func readRowsWithin(ctx context.Context, db *sql.DB, q query, maxBytes int) ([]map[string]any, bool, error) {
	if maxBytes < 2 {
		return []map[string]any{}, true, nil
	}
	limit := queryRowLimit(q.name)
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
	used := 2
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
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, false, fmt.Errorf("encode_diagnostic_row")
		}
		length := len(encoded)
		if len(result) > 0 {
			length++
		}
		if used+length > maxBytes {
			return result, true, nil
		}
		used += length
		result = append(result, item)
	}
	return result, false, rows.Err()
}
