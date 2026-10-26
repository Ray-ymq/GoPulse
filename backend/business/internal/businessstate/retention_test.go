package businessstate

import "testing"

func TestRetentionHeadroomUsesBothLimits(t *testing.T) {
	limits := BudgetLimits{RetainedGroups: 100, RetainedBytes: 1000}
	if retentionHeadroom(limits, 1, 1, 0, 0) != nil {
		t.Fatal("zero growth must be unknown")
	}
	if got := retentionHeadroom(limits, 50, 100, 10, 100); got == nil || *got != 18000 {
		t.Fatalf("remaining=%v", got)
	}
	if got := retentionHeadroom(limits, 101, 1, 1, 1); got == nil || *got != 0 {
		t.Fatal("exhausted budget")
	}
}
