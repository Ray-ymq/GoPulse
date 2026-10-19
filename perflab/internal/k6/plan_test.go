package k6

import "testing"

func TestBoundaryPlanUsesConfiguredDoublingAndIncludesMaximum(t *testing.T) {
	config := DefaultConfig()
	plan, err := BuildPlan(config, ModeBoundary, BoundaryRange{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Stages) != 8 || plan.Stages[0].TargetRPS != 10 || plan.Stages[6].TargetRPS != 640 || plan.Stages[7].TargetRPS != 1000 {
		t.Fatalf("boundary plan=%+v", plan.Stages)
	}
	if plan.Stages[0].WarmupSeconds != 30 || plan.Stages[0].MeasurementSeconds != 120 || plan.Stages[0].RecoverySeconds != 120 {
		t.Fatalf("boundary timing=%+v", plan.Stages[0])
	}
}

func TestRefineIntervalMovesOnlyTheObservedHalf(t *testing.T) {
	boundary := BoundaryRange{StableRPS: 80, UnstableRPS: 160, Found: true}
	boundary = nextRefineInterval(boundary, 120, true)
	if boundary.StableRPS != 120 || boundary.UnstableRPS != 160 {
		t.Fatalf("stable interval=%+v", boundary)
	}
	boundary = nextRefineInterval(boundary, 140, false)
	if boundary.StableRPS != 120 || boundary.UnstableRPS != 140 {
		t.Fatalf("unstable interval=%+v", boundary)
	}
}

func TestClassificationSeparatesUnstableAndInsufficientRecoveryEvidence(t *testing.T) {
	config := DefaultConfig()
	metrics := WindowMetrics{TargetRPS: 100, ActualRequestRate: 100, P95MS: 300, P99MS: 700, ErrorRate: 0.001}
	status, reasons := classify(metrics, RecoveryResult{Status: "not_recovered"}, config.Thresholds, true)
	if status != StatusUnstable || len(reasons) != 1 || reasons[0] != "backlog_not_recovered" {
		t.Fatalf("unstable status=%s reasons=%v", status, reasons)
	}
	status, reasons = classify(metrics, RecoveryResult{Status: "not_observed"}, config.Thresholds, false)
	if status != StatusCapacityInsufficient || len(reasons) != 1 || reasons[0] != "backlog_not_observed" {
		t.Fatalf("insufficient status=%s reasons=%v", status, reasons)
	}
}
