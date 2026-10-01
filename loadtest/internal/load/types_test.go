package load

import (
	"math"
	"os"
	"testing"
	"time"
)

func TestLatencySummaryUsesNearestRank(t *testing.T) {
	summary := summarize([]float64{1, 2, 3, 4, 5})
	if summary.P50MS != 3 || summary.P95MS != 5 || summary.P99MS != 5 || summary.MaxMS != 5 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestScheduleOffsetsHonorOpenLoopRate(t *testing.T) {
	first := fixedScheduleOffset(0, 10)
	second := fixedScheduleOffset(1, 10)
	if first != 0 || math.Abs(float64(second.Milliseconds()-100)) > 0.001 {
		t.Fatalf("offsets=%s %s", first, second)
	}
	ramp := rampScheduleOffset(50, 100_000_000_000, 100)
	if ramp <= 0 {
		t.Fatalf("ramp offset=%s", ramp)
	}
}

func TestCapacityProfileIsStrictlyBoundAndUsesThreeFixedRepetitions(t *testing.T) {
	encoded, err := os.ReadFile("../../capacity-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	profile, digest, err := LoadProfile("../../capacity-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Repetitions != 3 || len(profile.Stages) != 4 || profile.Stages[0].TargetRPS != 50 || profile.Stages[3].TargetRPS != 200 {
		t.Fatalf("profile=%+v", profile)
	}
	if digest != ProfileDigest(encoded) {
		t.Fatalf("profile digest=%s", digest)
	}
	profile.Repetitions = 2
	if err := ValidateProfile(profile); err == nil {
		t.Fatal("profile accepted repetition drift")
	}
}

func TestOutcomeSummaryKeeps429503TimeoutAndUnexpectedErrorSeparate(t *testing.T) {
	value := newCapacityAccumulator(100, time.Second)
	value.add(requestResult{status: 429, explicitReject: true, latencyMS: 1})
	value.add(requestResult{status: 503, explicitReject: true, latencyMS: 2})
	value.add(requestResult{timeout: true, transportFailure: true, latencyMS: 3})
	value.add(requestResult{status: 500, latencyMS: 4})
	if value.outcomes.ExplicitRejects != 2 || value.outcomes.Rejected429 != 1 || value.outcomes.Rejected503 != 1 || value.outcomes.Timeouts != 1 || value.outcomes.UnexpectedErrors != 1 || value.outcomes.TransportErrors != 0 {
		t.Fatalf("outcomes=%+v", value.outcomes)
	}
}

func TestDiagnosticProfileRequiresIndependentFrozenRecovery(t *testing.T) {
	profile, _, err := LoadProfile("../../phase20-capacity-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	if profile.ResourceBudget.Path != "deploy/phase20-resource-budgets.json" || profile.ResourceBudget.Schema != "deploy/phase20-resource-budgets.schema.json" || profile.ResourceBudget.ContractID != "phase20-05-budget-contract-20261002-r4" {
		t.Fatalf("resource budget=%+v", profile.ResourceBudget)
	}
	profile.Diagnostic.DrainSeconds = 31
	if ValidateProfile(profile) == nil {
		t.Fatal("accepted drain drift")
	}
	profile.Diagnostic.DrainSeconds = 30
	profile.SchemaVersion = CapacityProfileSchemaVersion
	if ValidateProfile(profile) == nil {
		t.Fatal("phase19 accepted phase20 semantics")
	}
}
