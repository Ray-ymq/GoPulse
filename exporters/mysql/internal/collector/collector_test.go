package collector

import (
	"strings"
	"testing"
)

func TestCompleteSnapshotAndFailure(t *testing.T) {
	values := map[string]string{}
	for _, f := range fields {
		values[f.upstream] = "12"
	}
	body, err := Render(values)
	if err != nil || strings.Count(body, "# TYPE ") != 29 || !strings.Contains(body, `gopulse_mysql_transactions_total{result="rollback"} 12`) || !strings.Contains(body, `gopulse_mysql_statements_total{operation="select"} 12`) || !strings.Contains(body, "gopulse_mysql_row_lock_wait_seconds_total 0.012") {
		t.Fatal("incomplete snapshot", err)
	}
	delete(values, "Com_commit")
	if body, err := Render(values); err != ErrUnavailable || body != "" {
		t.Fatal("partial snapshot leaked")
	}
}
