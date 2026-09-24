package load

import (
	"math"
	"testing"
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
