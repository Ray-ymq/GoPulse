package control

import "testing"

// Reproduces the first real recovery run's dated ES index rejection.
func TestSearchDatedIndexIdentity(t *testing.T) {
	if !searchIdentity.MatchString("gopulse-logs-v1-2026.09.13") {
		t.Fatal("real dated index rejected")
	}
	if searchIdentity.MatchString("gopulse-logs/../../other") {
		t.Fatal("non-index path accepted")
	}
}
