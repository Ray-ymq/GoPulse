package businessstate

import (
	"database/sql"
	"strings"
	"testing"
)

func TestRepositoryBudgetLimitDefaultsAndAcceptanceOverride(t *testing.T) {
	defaultRepository, err := NewRepository(&sql.DB{})
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	if got := defaultRepository.BudgetLimits().OpenRequirements; got != MaximumOpenRequirements {
		t.Fatalf("default open requirement limit = %d, want %d", got, MaximumOpenRequirements)
	}
	overridden, err := NewRepository(&sql.DB{}, WithOpenRequirementsLimit(8))
	if err != nil {
		t.Fatalf("NewRepository(8) error = %v", err)
	}
	if got := overridden.BudgetLimits().OpenRequirements; got != 8 {
		t.Fatalf("override open requirement limit = %d, want 8", got)
	}
	for _, limit := range []uint64{0, MaximumOpenRequirements + 1} {
		if _, err := NewRepository(&sql.DB{}, WithOpenRequirementsLimit(limit)); err == nil {
			t.Fatalf("accepted invalid open requirement limit %d", limit)
		}
	}
}

func TestIdentityValidationIsCanonicalAndBounded(t *testing.T) {
	if err := ValidateEpoch("123e4567-e89b-12d3-a456-426614174000"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		"123e4567-e89b-02d3-a456-426614174000",
		"123e4567-e89b-12d3-c456-426614174000",
		"123e4567-e89b-12d3-a456-42661417400Z",
		"123e4567-e89b-12d3-a456-426614174000/extra",
	} {
		if ValidateEpoch(value) == nil {
			t.Fatalf("accepted invalid epoch %q", value)
		}
	}
	if err := ValidateOperationID(strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if ValidateOperationID(strings.Repeat("A", 64)) == nil || ValidateOperationID(strings.Repeat("a", 63)) == nil {
		t.Fatal("accepted invalid operation ID")
	}
}

func TestSafeReasonAllowsOnlyStableSummaryCharacters(t *testing.T) {
	for _, value := range []string{"epoch_mismatch", "attempt-3", "mapping_rejected"} {
		if got := safeReason(value); got != value {
			t.Fatalf("safeReason(%q)=%q", value, got)
		}
	}
	for _, value := range []string{"", "HTTP 500", "secret@example", strings.Repeat("x", 65)} {
		if got := safeReason(value); got != "unknown" {
			t.Fatalf("safeReason(%q)=%q, want unknown", value, got)
		}
	}
}
