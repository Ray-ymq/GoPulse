package collector

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Performance Schema 的计时单位为皮秒，输出统一使用秒。
const picoSeconds = 1e-12

type diagnosticMetric struct {
	name, kind string
	scale      float64
}
type diagnosticQuery struct {
	name, query string
	labels      []string
	metrics     []diagnosticMetric
	limit       int
}

func gauge(name string) diagnosticMetric   { return diagnosticMetric{name, "gauge", 1} }
func counter(name string) diagnosticMetric { return diagnosticMetric{name, "counter", 1} }
func seconds(name string) diagnosticMetric { return diagnosticMetric{name, "counter", picoSeconds} }

const InnoDBTransactionsQuery = `SELECT trx_state, COUNT(*), COALESCE(MAX(GREATEST(TIMESTAMPDIFF(MICROSECOND,trx_started,NOW(6))/1000000,0)),0), COALESCE(MAX(GREATEST(TIMESTAMPDIFF(MICROSECOND,trx_wait_started,NOW(6))/1000000,0)),0), SUM(trx_rows_locked), SUM(trx_rows_modified) FROM information_schema.innodb_trx GROUP BY trx_state`
const lockWaitsQuery = `SELECT COALESCE(l.OBJECT_SCHEMA,''), COALESCE(l.OBJECT_NAME,''), COALESCE(l.INDEX_NAME,''), COUNT(DISTINCT w.REQUESTING_ENGINE_TRANSACTION_ID), COUNT(DISTINCT w.BLOCKING_ENGINE_TRANSACTION_ID), COALESCE(MAX(GREATEST(TIMESTAMPDIFF(MICROSECOND,t.trx_wait_started,NOW(6))/1000000,0)),0) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE=w.ENGINE AND l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID LEFT JOIN information_schema.innodb_trx t ON t.trx_id=w.REQUESTING_ENGINE_TRANSACTION_ID WHERE l.LOCK_TYPE='RECORD' GROUP BY l.OBJECT_SCHEMA,l.OBJECT_NAME,l.INDEX_NAME ORDER BY 6 DESC,1,2,3 LIMIT 129`

var diagnosticQueries = []diagnosticQuery{
	{"innodb_metrics", InnoDBMetricsQuery, nil, nil, 0},
	{"transactions", InnoDBTransactionsQuery, []string{"state"}, []diagnosticMetric{gauge("transactions_by_state"), gauge("transaction_state_oldest_age_seconds"), gauge("transaction_state_longest_lock_wait_seconds"), gauge("transaction_state_rows_locked"), gauge("transaction_state_rows_modified")}, 0},
	{"redo_capacity", RedoCapacityQuery, nil, []diagnosticMetric{gauge("redo_log_capacity_bytes")}, 0},
	{"buffer_pool", `SELECT @@GLOBAL.innodb_buffer_pool_size, (SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Innodb_buffer_pool_wait_free')`, nil, []diagnosticMetric{gauge("buffer_pool_capacity_bytes"), counter("buffer_pool_wait_free_total")}, 0},
	{"lock_waits", lockWaitsQuery, []string{"schema", "table", "index"}, []diagnosticMetric{gauge("row_lock_waiting_transactions"), gauge("row_lock_blocking_transactions"), gauge("row_lock_longest_wait_seconds")}, 128},
	{"statement_digest", `SELECT COALESCE(SCHEMA_NAME,''), COALESCE(DIGEST,''), COUNT_STAR,SUM_TIMER_WAIT,SUM_ERRORS,SUM_ROWS_EXAMINED,SUM_ROWS_SENT,SUM_CREATED_TMP_DISK_TABLES,SUM_NO_INDEX_USED FROM performance_schema.events_statements_summary_by_digest ORDER BY SUM_TIMER_WAIT DESC,SCHEMA_NAME,DIGEST LIMIT 513`, []string{"schema", "digest"}, []diagnosticMetric{counter("statement_digest_executions_total"), seconds("statement_digest_seconds_total"), counter("statement_digest_errors_total"), counter("statement_digest_rows_examined_total"), counter("statement_digest_rows_sent_total"), counter("statement_digest_temporary_disk_tables_total"), counter("statement_digest_no_index_used_total")}, 512},
	{"statement_events", `SELECT EVENT_NAME,COUNT_STAR,SUM_TIMER_WAIT,SUM_ERRORS FROM performance_schema.events_statements_summary_global_by_event_name WHERE EVENT_NAME IN ('statement/com/Prepare','statement/com/Execute','statement/com/Close stmt','statement/sql/prepare_sql','statement/sql/execute_sql','statement/sql/commit','statement/com/Commit') ORDER BY EVENT_NAME`, []string{"event"}, []diagnosticMetric{counter("statement_event_executions_total"), seconds("statement_event_seconds_total"), counter("statement_event_errors_total")}, 0},
	{"errors", `SELECT COALESCE(CAST(ERROR_NUMBER AS CHAR),'unknown'),COALESCE(ERROR_NAME,'unknown'),SUM_ERROR_RAISED FROM performance_schema.events_errors_summary_global_by_error WHERE SUM_ERROR_RAISED>0 OR ERROR_NUMBER IN (1205,1213,1317) ORDER BY (ERROR_NUMBER IN (1205,1213,1317)) DESC,SUM_ERROR_RAISED DESC,ERROR_NUMBER LIMIT 257`, []string{"code", "error"}, []diagnosticMetric{counter("errors_total")}, 256},
	{"file_io", `SELECT CASE WHEN EVENT_NAME LIKE 'wait/io/file/sql/binlog%' THEN 'binlog' WHEN EVENT_NAME LIKE '%/innodb_log_file' THEN 'redo' WHEN EVENT_NAME LIKE '%/innodb_data_file' THEN 'data' ELSE 'other' END,EVENT_NAME,COUNT_READ,SUM_TIMER_READ,SUM_NUMBER_OF_BYTES_READ,COUNT_WRITE,SUM_TIMER_WRITE,SUM_NUMBER_OF_BYTES_WRITE,COUNT_MISC,SUM_TIMER_MISC FROM performance_schema.file_summary_by_event_name WHERE (EVENT_NAME LIKE 'wait/io/file/innodb/%' OR EVENT_NAME LIKE 'wait/io/file/sql/binlog%') AND COUNT_STAR>0 ORDER BY SUM_TIMER_WAIT DESC,EVENT_NAME LIMIT 65`, []string{"file_type", "event"}, []diagnosticMetric{counter("file_reads_total"), seconds("file_read_seconds_total"), counter("file_read_bytes_total"), counter("file_writes_total"), seconds("file_write_seconds_total"), counter("file_write_bytes_total"), counter("file_misc_operations_total"), seconds("file_misc_seconds_total")}, 64},
	{"wait_events", `SELECT EVENT_NAME,COUNT_STAR,SUM_TIMER_WAIT FROM performance_schema.events_waits_summary_global_by_event_name WHERE (EVENT_NAME LIKE 'wait/synch/mutex/%' OR EVENT_NAME LIKE 'wait/synch/cond/%' OR EVENT_NAME LIKE 'wait/synch/rwlock/%' OR EVENT_NAME LIKE 'wait/synch/sxlock/%') AND COUNT_STAR>0 ORDER BY SUM_TIMER_WAIT DESC,EVENT_NAME LIMIT 129`, []string{"event"}, []diagnosticMetric{counter("wait_events_total"), seconds("wait_event_seconds_total")}, 128},
	tableAccessQuery("table_io", "table_io_waits_summary_by_table", false),
	tableAccessQuery("index_io", "table_io_waits_summary_by_index_usage", true),
	{"performance_schema", `SELECT @@GLOBAL.performance_schema`, nil, []diagnosticMetric{gauge("performance_schema_enabled")}, 0},
	{"consumers", `SELECT NAME,IF(ENABLED='YES',1,0) FROM performance_schema.setup_consumers ORDER BY NAME`, []string{"consumer"}, []diagnosticMetric{gauge("performance_schema_consumer_enabled")}, 0},
	{"instruments", `SELECT SUBSTRING_INDEX(NAME,'/',3),COUNT(*),SUM(ENABLED='YES'),SUM(TIMED='YES') FROM performance_schema.setup_instruments WHERE NAME LIKE 'statement/%' OR NAME LIKE 'wait/io/file/%' OR NAME LIKE 'wait/io/table/%' OR NAME LIKE 'wait/synch/%' GROUP BY SUBSTRING_INDEX(NAME,'/',3) ORDER BY 1`, []string{"instrument_group"}, []diagnosticMetric{gauge("performance_schema_instruments"), gauge("performance_schema_instruments_enabled"), gauge("performance_schema_instruments_timed")}, 0},
	{"digest_lost", `SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Performance_schema_digest_lost'`, nil, []diagnosticMetric{counter("performance_schema_digest_lost_total")}, 0},
}

func tableAccessQuery(name, table string, index bool) diagnosticQuery {
	labels := []string{"schema", "table"}
	columns := "COALESCE(OBJECT_SCHEMA,''),COALESCE(OBJECT_NAME,'')"
	order := "OBJECT_SCHEMA,OBJECT_NAME"
	if index {
		labels = append(labels, "index")
		columns += ",COALESCE(INDEX_NAME,'')"
		order += ",INDEX_NAME"
	}
	metrics := []diagnosticMetric{}
	for _, op := range []string{"fetch", "insert", "update", "delete"} {
		columns += ",COUNT_" + strings.ToUpper(op) + ",SUM_TIMER_" + strings.ToUpper(op)
		metrics = append(metrics, counter(name+"_"+op+"_total"), seconds(name+"_"+op+"_seconds_total"))
	}
	return diagnosticQuery{name, "SELECT " + columns + " FROM performance_schema." + table + " WHERE COUNT_STAR>0 ORDER BY SUM_TIMER_WAIT DESC," + order + " LIMIT 129", labels, metrics, 128}
}

func (c *Client) collectDiagnostics(ctx context.Context) string {
	var out strings.Builder
	c.collectDiagnosticsTo(ctx, &out)
	return out.String()
}

func (c *Client) collectDiagnosticsTo(ctx context.Context, out *strings.Builder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failures == nil {
		c.failures = map[string]uint64{}
	}
	for _, q := range diagnosticQueries {
		enabled := c.collectors == "" || strings.Contains(","+c.collectors+",", ","+q.name+",")
		success, truncated := false, false
		started := time.Now()
		if enabled {
			body, cut, err := collectDiagnostic(ctx, c.DB, q)
			success = err == nil
			truncated = cut
			if success {
				out.WriteString(body)
			} else {
				c.failures[q.name]++
			}
		}
		label := `{collector=` + strconv.Quote(q.name) + `}`
		writeMetric(out, "collector_enabled", "gauge", label, boolNumber(enabled))
		writeMetric(out, "collector_success", "gauge", label, boolNumber(success))
		duration := 0.0
		if enabled {
			duration = time.Since(started).Seconds()
		}
		writeMetric(out, "collector_duration_seconds", "gauge", label, duration)
		writeMetric(out, "collector_failures_total", "counter", label, float64(c.failures[q.name]))
		writeMetric(out, "collector_truncated", "gauge", label, boolNumber(truncated))
	}
}
func boolNumber(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
func writeMetric(b *strings.Builder, name, kind, labels string, value float64) {
	if !strings.Contains(b.String(), "# TYPE gopulse_mysql_"+name+" ") {
		fmt.Fprintf(b, "# TYPE gopulse_mysql_%s %s\n", name, kind)
	}
	fmt.Fprintf(b, "gopulse_mysql_%s%s %s\n", name, labels, strconv.FormatFloat(value, 'g', -1, 64))
}

func collectDiagnostic(ctx context.Context, db *sql.DB, q diagnosticQuery) (string, bool, error) {
	rows, err := db.QueryContext(ctx, q.query)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = rows.Close() }()
	var out strings.Builder
	count := 0
	truncated := false
	byteLimit := 64 << 10
	if q.name == "statement_digest" {
		byteLimit = 768 << 10
	}
	totals := make([]float64, 5)
	states := map[string]bool{}
	seen := map[string]bool{}
	for rows.Next() {
		count++
		if q.limit > 0 && (count > q.limit || out.Len() >= byteLimit) {
			truncated = true
			continue
		}
		if q.name == "innodb_metrics" {
			var name, status string
			var value sql.NullString
			if err := rows.Scan(&name, &value, &status); err != nil {
				return "", false, err
			}
			if status != "enabled" {
				continue
			}
			for _, f := range optionalFields {
				if f.upstream == name && name != "active_transactions" {
					n, err := diagnosticNumber(value)
					if err != nil {
						return "", false, err
					}
					writeMetric(&out, f.name, f.kind, "", n)
				}
			}
			continue
		}
		raw := make([]sql.NullString, len(q.labels)+len(q.metrics))
		dest := make([]any, len(raw))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return "", false, err
		}
		labels := make([]string, len(q.labels))
		for i, key := range q.labels {
			v := raw[i]
			if !v.Valid || !utf8.ValidString(v.String) || len(v.String) > 256 || strings.ContainsRune(v.String, 0) {
				return "", false, ErrUnavailable
			}
			labels[i] = key + "=" + quoteLabel(v.String)
		}
		label := ""
		if len(labels) > 0 {
			label = "{" + strings.Join(labels, ",") + "}"
		}
		if seen[label] {
			return "", false, ErrUnavailable
		}
		seen[label] = true
		nums := make([]float64, len(q.metrics))
		for i, m := range q.metrics {
			n, err := diagnosticNumber(raw[len(q.labels)+i])
			if err != nil {
				return "", false, err
			}
			nums[i] = n
			metricName, metricLabel := m.name, label
			if q.name == "table_io" || q.name == "index_io" {
				operation := []string{"fetch", "insert", "update", "delete"}[i/2]
				metricLabel = strings.TrimSuffix(label, "}") + `,operation="` + operation + `"}`
				metricName = q.name + "_operations_total"
				if i%2 == 1 {
					metricName = q.name + "_seconds_total"
				}
			}
			writeMetric(&out, metricName, m.kind, metricLabel, n*m.scale)
		}
		if q.name == "transactions" {
			states[raw[0].String] = true
			totals[0] += nums[0]
			totals[1] = math.Max(totals[1], nums[1])
			totals[2] = math.Max(totals[2], nums[2])
			totals[3] += nums[3]
			totals[4] += nums[4]
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if q.name == "transactions" {
		for _, state := range []string{"RUNNING", "LOCK WAIT", "COMMITTING", "ROLLING BACK"} {
			if !states[state] {
				for _, m := range q.metrics {
					writeMetric(&out, m.name, m.kind, `{state=`+strconv.Quote(state)+`}`, 0)
				}
			}
		}
		for i, name := range []string{"active_transactions", "transaction_oldest_age_seconds", "transaction_longest_lock_wait_seconds", "transaction_rows_locked", "transaction_rows_modified"} {
			writeMetric(&out, name, "gauge", "", totals[i])
		}
	}
	if count == 0 && q.labels == nil && q.name != "innodb_metrics" {
		return "", false, ErrUnavailable
	}
	return out.String(), truncated, nil
}
func diagnosticNumber(raw sql.NullString) (float64, error) {
	if !raw.Valid {
		return 0, ErrUnavailable
	}
	n, err := strconv.ParseFloat(raw.String, 64)
	if err != nil || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, ErrUnavailable
	}
	return n, nil
}

func quoteLabel(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value) + `"`
}
