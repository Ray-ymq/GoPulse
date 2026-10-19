package k6

import (
	"fmt"
	"math"
)

type StagePlan struct {
	Name               string  `json:"name"`
	TargetRPS          float64 `json:"target_rps"`
	WarmupSeconds      float64 `json:"warmup_seconds"`
	MeasurementSeconds float64 `json:"measurement_seconds"`
	RecoverySeconds    float64 `json:"recovery_seconds"`
}

type Plan struct {
	Mode   Mode        `json:"mode"`
	Stages []StagePlan `json:"stages"`
}

type BoundaryRange struct {
	StableRPS   float64
	UnstableRPS float64
	Found       bool
}

func BuildPlan(config Config, mode Mode, boundary BoundaryRange) (Plan, error) {
	switch mode {
	case ModeBoundary:
		return Plan{Mode: mode, Stages: boundaryStages(config.Boundary)}, nil
	case ModeRefine:
		if !boundary.Found || boundary.StableRPS <= 0 || boundary.UnstableRPS <= boundary.StableRPS {
			return Plan{}, fmt.Errorf("refine requires a boundary report with stable and unstable rates")
		}
		return Plan{Mode: mode, Stages: refineStages(config.Refine, boundary)}, nil
	case ModeSoak:
		if !boundary.Found || boundary.StableRPS <= 0 {
			return Plan{}, fmt.Errorf("soak requires a report with a confirmed stable capacity")
		}
		target := boundary.StableRPS * config.Soak.StableCapacityRatio
		return Plan{Mode: mode, Stages: []StagePlan{{
			Name: "soak", TargetRPS: target, WarmupSeconds: 0,
			MeasurementSeconds: config.Soak.DurationSeconds, RecoverySeconds: config.Soak.RecoverySeconds,
		}}}, nil
	default:
		return Plan{}, fmt.Errorf("unsupported load mode %q", mode)
	}
}

func boundaryStages(config BoundaryConfig) []StagePlan {
	stages := make([]StagePlan, 0, 16)
	for target := config.StartRPS; target < config.MaxRPS; target *= config.Multiplier {
		stages = append(stages, StagePlan{
			Name: fmt.Sprintf("boundary-rps-%g", target), TargetRPS: target,
			WarmupSeconds: config.WarmupSeconds, MeasurementSeconds: config.MeasurementSeconds,
			RecoverySeconds: config.RecoverySeconds,
		})
		if target > config.MaxRPS/config.Multiplier {
			break
		}
	}
	stages = append(stages, StagePlan{
		Name: fmt.Sprintf("boundary-rps-%g", config.MaxRPS), TargetRPS: config.MaxRPS,
		WarmupSeconds: config.WarmupSeconds, MeasurementSeconds: config.MeasurementSeconds,
		RecoverySeconds: config.RecoverySeconds,
	})
	return stages
}

func refineStages(config RefineConfig, boundary BoundaryRange) []StagePlan {
	low, high := boundary.StableRPS, boundary.UnstableRPS
	if high-low <= low*config.MaxRelativeWidth {
		return nil
	}
	mid := math.Round((low+high)/2*100) / 100
	if mid <= low || mid >= high {
		return nil
	}
	return []StagePlan{{
		Name: fmt.Sprintf("refine-rps-%g", mid), TargetRPS: mid,
		MeasurementSeconds: config.MeasurementSeconds, RecoverySeconds: config.RecoverySeconds,
	}}
}

func nextRefineInterval(boundary BoundaryRange, target float64, stable bool) BoundaryRange {
	if stable && target > boundary.StableRPS && target < boundary.UnstableRPS {
		boundary.StableRPS = target
	} else if !stable && target > boundary.StableRPS && target < boundary.UnstableRPS {
		boundary.UnstableRPS = target
	}
	return boundary
}

func boundaryFromResults(results []StageResult) BoundaryRange {
	var stable, unstable float64
	for _, result := range results {
		if result.Status == StatusStable && result.TargetRPS > stable {
			stable = result.TargetRPS
		}
		if result.Status == StatusUnstable && (unstable == 0 || result.TargetRPS < unstable) {
			unstable = result.TargetRPS
		}
	}
	return BoundaryRange{StableRPS: stable, UnstableRPS: unstable, Found: stable > 0 && unstable > stable}
}
