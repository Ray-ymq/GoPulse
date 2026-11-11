package collector

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

type diagnosticConnector struct {
	responses  map[string]diagnosticConnector
	values     [][]driver.Value
	columns    int
	rowError   bool
	queryError bool
}

func (c diagnosticConnector) Connect(context.Context) (driver.Conn, error) {
	return diagnosticConn{c}, nil
}
func (c diagnosticConnector) Driver() driver.Driver { return diagnosticDriver{} }

type diagnosticDriver struct{}

func (diagnosticDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unsupported") }

type diagnosticConn struct{ diagnosticConnector }

func (diagnosticConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (diagnosticConn) Close() error                        { return nil }
func (diagnosticConn) Begin() (driver.Tx, error)           { return nil, errors.New("unsupported") }
func (c diagnosticConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.responses != nil {
		response, ok := c.responses[query]
		if !ok {
			return nil, errors.New("denied")
		}
		return (diagnosticConn{response}).QueryContext(context.Background(), query, nil)
	}
	if c.queryError {
		return nil, errors.New("denied")
	}
	return &diagnosticRows{c.values, c.columns, c.rowError, 0}, nil
}

type diagnosticRows struct {
	values   [][]driver.Value
	columns  int
	rowError bool
	position int
}

func (r *diagnosticRows) Columns() []string { return make([]string, r.columns) }
func (*diagnosticRows) Close() error        { return nil }
func (r *diagnosticRows) Next(dest []driver.Value) error {
	if r.position == len(r.values) {
		if r.rowError {
			return errors.New("interrupted")
		}
		return io.EOF
	}
	copy(dest, r.values[r.position])
	r.position++
	return nil
}
func queryNamed(name string) diagnosticQuery {
	for _, q := range diagnosticQueries {
		if q.name == name {
			return q
		}
	}
	panic(name)
}
func testDB(t *testing.T, c diagnosticConnector) *sql.DB {
	t.Helper()
	db := sql.OpenDB(c)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
func assertContains(t *testing.T, body string, samples ...string) {
	t.Helper()
	for _, s := range samples {
		if !strings.Contains(body, s) {
			t.Fatalf("缺少指标 %s", s)
		}
	}
}

func TestDiagnosticQueriesSkipInactiveRows(t *testing.T) {
	for _, name := range []string{"file_io", "wait_events", "table_io", "index_io"} {
		q := queryNamed(name)
		if !strings.Contains(q.query, "COUNT_STAR>0") {
			t.Fatalf("%s 未过滤未启用或未发生的统计行", name)
		}
	}
	if q := queryNamed("wait_events"); !strings.Contains(q.query, "wait/synch/sxlock/%") {
		t.Fatal("wait_events 未覆盖 sxlock")
	}
}

func TestTransactionStatesAndEmptySnapshot(t *testing.T) {
	q := queryNamed("transactions")
	db := testDB(t, diagnosticConnector{columns: 6, values: [][]driver.Value{{"RUNNING", "2", "8.25", "0", "4", "3"}, {"LOCK WAIT", "3", "12.5", "6.75", "5", "1"}, {"COMMITTING", "1", "1", "0", "0", "0"}}})
	body, _, err := collectDiagnostic(t.Context(), db, q)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, body, `transactions_by_state{state="LOCK WAIT"} 3`, `transaction_state_oldest_age_seconds{state="RUNNING"} 8.25`, "active_transactions 6", "transaction_oldest_age_seconds 12.5", "transaction_longest_lock_wait_seconds 6.75", "transaction_rows_locked 9", `transactions_by_state{state="ROLLING BACK"} 0`)
	body, _, err = collectDiagnostic(t.Context(), testDB(t, diagnosticConnector{columns: 6}), q)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, body, "active_transactions 0", "transaction_oldest_age_seconds 0")
}
func TestDiagnosticAtomicFailureAndValidation(t *testing.T) {
	q := queryNamed("statement_events")
	for _, c := range []diagnosticConnector{
		{columns: 4, values: [][]driver.Value{{"statement/com/Execute", "2", "1000000000000", "1"}}, rowError: true},
		{columns: 4, values: [][]driver.Value{{"statement/com/Execute", "2", "-1", "1"}}},
		{columns: 4, values: [][]driver.Value{{"statement/com/Execute", "2", nil, "1"}}},
		{columns: 4, values: [][]driver.Value{{"statement/com/Execute", "2", "NaN", "1"}}},
		{columns: 4, values: [][]driver.Value{{"statement/com/Execute", "2", "1", "1"}, {"statement/com/Execute", "2", "1", "1"}}},
		{queryError: true},
	} {
		body, _, err := collectDiagnostic(t.Context(), testDB(t, c), q)
		if err == nil || body != "" {
			t.Fatal("失败采集泄漏了部分指标")
		}
	}
}
func TestDigestScalingEscapingAndTruncation(t *testing.T) {
	q := queryNamed("statement_digest")
	q.limit = 1
	row := []driver.Value{"db\"\\\n", "abc", "3", "2500000000000", "1", "6", "2", "4", "5"}
	body, cut, err := collectDiagnostic(t.Context(), testDB(t, diagnosticConnector{columns: 9, values: [][]driver.Value{row, row}}), q)
	if err != nil || !cut {
		t.Fatal(err, cut)
	}
	assertContains(t, body, `statement_digest_seconds_total{schema="db\"\\\n",digest="abc"} 2.5`)
	e := taskExporter(namedScrape{"test", func(ctx context.Context, _ *Client, ch chan<- prometheus.Metric) error {
		return emitText(ctx, "# TYPE gopulse_mysql_up gauge\ngopulse_mysql_up 1\n"+body, ch)
	}})
	if out := requestMetrics(e); out.Code != 200 {
		t.Fatal(out.Body.String())
	}
}

func TestDigestCoverageAndBounds(t *testing.T) {
	q := queryNamed("statement_digest")
	values := make([][]driver.Value, 513)
	for i := range values {
		digest := strings.Repeat("0", 64-len(strconv.Itoa(i))) + strconv.Itoa(i)
		values[i] = []driver.Value{"gopulse_pt", digest, "3", "2500000000000", "1", "6", "2", "4", "5"}
	}
	for _, count := range []int{300, 512, 513} {
		body, cut, err := collectDiagnostic(t.Context(), testDB(t, diagnosticConnector{columns: 9, values: values[:count]}), q)
		if err != nil || cut != (count > 512) || strings.Count(body, "\ngopulse_mysql_statement_digest_executions_total{") != min(count, 512) {
			t.Fatalf("digest 覆盖或组数上限不符：count=%d cut=%v err=%v", count, cut, err)
		}
		if len(body) >= maxBodyBytes {
			t.Fatal("常规 digest 标签超出响应上限")
		}
	}
	for i := range values {
		values[i][0] = strings.Repeat("s", 256)
		values[i][1] = strings.Repeat("d", 250) + strconv.Itoa(i)
	}
	body, cut, err := collectDiagnostic(t.Context(), testDB(t, diagnosticConnector{columns: 9, values: values}), q)
	if err != nil || !cut || len(body) > (768<<10)+(8<<10) {
		t.Fatalf("digest 字节上限不符：bytes=%d cut=%v err=%v", len(body), cut, err)
	}
}
func TestCollectorFailureCountRecoveryAndDisabled(t *testing.T) {
	c := &Client{DB: testDB(t, diagnosticConnector{queryError: true}), collectors: "digest_lost"}
	for i := 1; i <= 2; i++ {
		body := c.collectDiagnostics(t.Context())
		assertContains(t, body, `collector_enabled{collector="digest_lost"} 1`, `collector_success{collector="digest_lost"} 0`, `collector_failures_total{collector="digest_lost"} `+strconv.Itoa(i), `collector_enabled{collector="transactions"} 0`)
	}
	c.DB = testDB(t, diagnosticConnector{columns: 1, values: [][]driver.Value{{"7"}}})
	body := c.collectDiagnostics(t.Context())
	assertContains(t, body, `collector_success{collector="digest_lost"} 1`, `collector_failures_total{collector="digest_lost"} 2`, "performance_schema_digest_lost_total 7")
}

// 显式指定本地测试容器后，只读取元数据和统计信息，不修改 MySQL 配置或业务表。
func TestMySQLDiagnosticQueriesIntegration(t *testing.T) {
	container := os.Getenv("GOPULSE_MYSQL_TEST_CONTAINER")
	if container == "" {
		t.Skip("未指定 MySQL 测试容器")
	}
	var body strings.Builder
	body.WriteString("# TYPE gopulse_mysql_up gauge\ngopulse_mysql_up 1\n")
	for _, q := range diagnosticQueries {
		cmd := exec.CommandContext(t.Context(), "docker", "exec", "-i", container, "sh", "-c", `MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot --batch --skip-column-names`)
		cmd.Stdin = strings.NewReader(q.query + ";\n")
		data, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s 查询失败: %v", q.name, err)
		}
		cols := len(q.labels) + len(q.metrics)
		if q.name == "innodb_metrics" {
			cols = 3
		}
		values := [][]driver.Value{}
		for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
			if line == "" {
				continue
			}
			row := []driver.Value{}
			for _, v := range strings.Split(line, "\t") {
				if v == "NULL" {
					row = append(row, nil)
				} else {
					row = append(row, v)
				}
			}
			if len(row) != cols {
				t.Fatalf("%s 列数错误", q.name)
			}
			values = append(values, row)
		}
		result, cut, err := collectDiagnostic(t.Context(), testDB(t, diagnosticConnector{columns: cols, values: values}), q)
		if err != nil {
			t.Fatalf("%s 转换失败: %v", q.name, err)
		}
		body.WriteString(result)
		t.Logf("%s: rows=%d truncated=%v", q.name, len(values), cut)
	}
	e := taskExporter(namedScrape{"integration", func(ctx context.Context, _ *Client, ch chan<- prometheus.Metric) error {
		return emitText(ctx, body.String(), ch)
	}})
	if out := requestMetrics(e); out.Code != 200 {
		t.Fatalf("指标编码失败: %s", out.Body.String())
	}
}

func TestCollectPreservesUpAndReportsOptionalFailures(t *testing.T) {
	values := [][]driver.Value{}
	seen := map[string]bool{}
	for _, f := range fields {
		if f.upstream != "max_connections" && !seen[f.upstream] {
			values = append(values, []driver.Value{f.upstream, "12"})
			seen[f.upstream] = true
		}
	}
	c := &Client{DB: testDB(t, diagnosticConnector{queryError: true})}
	if _, err := Collect(t.Context(), c); err == nil {
		t.Fatal("基础采集失败未返回错误")
	}
	c.DB = testDB(t, diagnosticConnector{responses: map[string]diagnosticConnector{
		StatusQuery:                       {columns: 2, values: values},
		"SELECT @@GLOBAL.max_connections": {columns: 1, values: [][]driver.Value{{"100"}}},
	}})
	body, err := Collect(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, body, "gopulse_mysql_up 1", `collector_success{collector="global_status"} 1`, `collector_failures_total{collector="global_status"} 1`, `collector_enabled{collector="statement_digest"} 1`, `collector_success{collector="statement_digest"} 0`, `collector_failures_total{collector="statement_digest"} 1`)
	e := taskExporter(namedScrape{"snapshot", scrapeSnapshot})
	e.client = c
	if out := requestMetrics(e); out.Code != 200 {
		t.Fatal(out.Body.String())
	}
}

func TestDiagnosticByteBudgetAndOperationLabels(t *testing.T) {
	q := queryNamed("index_io")
	values := [][]driver.Value{}
	for i := 0; i < 128; i++ {
		row := []driver.Value{strings.Repeat("s", 256), strings.Repeat("t", 256), strings.Repeat("i", 250) + strconv.Itoa(i)}
		for j := 0; j < 8; j++ {
			row = append(row, "1000000000000")
		}
		values = append(values, row)
	}
	body, cut, err := collectDiagnostic(t.Context(), testDB(t, diagnosticConnector{columns: 11, values: values}), q)
	if err != nil || !cut || len(body) > 80<<10 {
		t.Fatal("正文上限失效", len(body), cut, err)
	}
	assertContains(t, body, `operation="fetch"} 1e+12`, `operation="insert"} 1`, `operation="update"} 1`, `operation="delete"} 1`)
}
