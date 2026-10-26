package retention

import (
	"testing"
	"time"
)

func TestPolicyUsesStrictUTCCalendarBoundary(t *testing.T) {
	p := DefaultConfig().Logs
	now := time.Date(2026, time.September, 30, 23, 59, 59, 999999999, time.UTC)
	cutoff := p.Cutoff(now)
	if got := cutoff.Format(DateLayout); got != "2026.09.24" {
		t.Fatalf("cutoff=%s, want 2026.09.24", got)
	}
	for _, test := range []struct {
		name, index, reason string
		eligible            bool
	}{
		{"before cutoff", "gopulse-logs-v1-2026.09.23", "expired", true},
		{"cutoff", "gopulse-logs-v1-2026.09.24", "not_expired", false},
		{"after cutoff", "gopulse-logs-v1-2026.09.25", "not_expired", false},
		{"future", "gopulse-logs-v1-2099.01.01", "not_expired", false},
		{"invalid date", "gopulse-logs-v1-2026.02.30", "invalid_date", false},
		{"foreign", "gopulse-post-search-v1-2026.09.23", "not_owned", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := p.Classify(test.index, now)
			if got.Reason != test.reason || got.Eligible != test.eligible {
				t.Fatalf("decision=%+v, want reason=%s eligible=%t", got, test.reason, test.eligible)
			}
		})
	}
	if !p.ExpiredTimestamp(time.Date(2026, 9, 23, 23, 59, 59, 999999999, time.UTC), now) {
		t.Fatal("date before cutoff was not expired")
	}
	if p.ExpiredTimestamp(cutoff.Add(1*time.Nanosecond), now) {
		t.Fatal("cutoff date was expired")
	}
}

func TestPolicyRejectsUnboundedConfiguration(t *testing.T) {
	p := DefaultConfig().Logs
	for _, days := range []int{0, MaxDays + 1} {
		p.RetentionDays = days
		if p.Validate() == nil {
			t.Fatalf("retention days %d was accepted", days)
		}
	}
}
