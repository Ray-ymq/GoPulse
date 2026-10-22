// Package flowload 管理固定流程的负载计划和业务判定。
package flowload

import (
	"fmt"
	"math"
)

type Stage struct {
	Name        string `json:"name"`
	Target      int    `json:"target_rps"`
	Warmup      int    `json:"warmup_seconds"`
	Measurement int    `json:"measurement_seconds"`
}

func Scan(start, limit, warmup, measure int) ([]Stage, error) {
	if start < 1 || start > limit || limit > 1000000 || warmup < 0 || measure < 1 {
		return nil, fmt.Errorf("起始 RPS 或时间无效")
	}
	stages := []Stage{}
	for target := start; ; target = min(limit, target*2) {
		stages = append(stages, Stage{fmt.Sprintf("scan-%d", target), target, warmup, measure})
		if target == limit {
			break
		}
	}
	return stages, nil
}

func Refine(low, high int, width float64) *Stage {
	if low <= 0 || high <= low || float64(high-low)/float64(low) <= width {
		return nil
	}
	mid := int(math.Floor(float64(low+high) / 2))
	if mid <= low || mid >= high {
		return nil
	}
	return &Stage{Name: fmt.Sprintf("refine-%d", mid), Target: mid, Warmup: 30, Measurement: 300}
}
