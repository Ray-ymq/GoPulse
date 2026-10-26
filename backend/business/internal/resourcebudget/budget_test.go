package resourcebudget

import "testing"

func TestBudgetAcquiresNonBlockingAndReleasesExactlyOnce(t *testing.T) {
	budget, err := New(Config{Auth: 1, Read: 1, Write: 1, Database: 1, Rebuild: 1, Waiter: 1})
	if err != nil {
		t.Fatal(err)
	}
	release, ok := budget.TryAcquire(Read)
	if !ok || budget.InFlight(Read) != 1 {
		t.Fatalf("first acquire ok=%t in_flight=%d", ok, budget.InFlight(Read))
	}
	if _, ok := budget.TryAcquire(Read); ok {
		t.Fatal("second acquire waited or succeeded")
	}
	release()
	release()
	if budget.InFlight(Read) != 0 {
		t.Fatalf("in_flight=%d after idempotent release", budget.InFlight(Read))
	}
}

func TestBudgetRejectsZeroCapacity(t *testing.T) {
	if _, err := New(Config{Auth: 0, Read: 1, Write: 1, Database: 1, Rebuild: 1, Waiter: 1}); err != ErrInvalidBudget {
		t.Fatalf("error=%v", err)
	}
}
