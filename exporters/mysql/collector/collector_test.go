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

func TestOptionalSecondBatchMetricsDoNotChangeBaseSnapshotShape(t *testing.T) {
	values := map[string]string{}
	for _, f := range fields {
		values[f.upstream] = "12"
	}
	optional := map[string]string{
		"lock_deadlocks":                 "3",
		"trx_rseg_history_len":           "7",
		"active_transactions":            "2",
		"transaction_oldest_age_seconds": "91",
		"transaction_rows_locked":        "5",
		"transaction_rows_modified":      "4",
		"log_lsn_current":                "100",
		"log_lsn_last_checkpoint":        "80",
		"log_lsn_checkpoint_age":         "20",
		"redo_log_capacity_bytes":        "1048576",
	}
	body, err := render(values, optional)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []string{
		"gopulse_mysql_deadlocks_total 3",
		"gopulse_mysql_undo_history_length 7",
		"gopulse_mysql_active_transactions 2",
		"gopulse_mysql_transaction_oldest_age_seconds 91",
		"gopulse_mysql_transaction_rows_locked 5",
		"gopulse_mysql_redo_checkpoint_age_bytes 20",
		"gopulse_mysql_redo_log_capacity_bytes 1.048576e+06",
	} {
		if !strings.Contains(body, sample) {
			t.Fatalf("missing %q in:\n%s", sample, body)
		}
	}
	if strings.Count(body, "# TYPE gopulse_mysql_") != 39 {
		t.Fatalf("unexpected metric family count: %d", strings.Count(body, "# TYPE gopulse_mysql_"))
	}
}
