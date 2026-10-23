//go:build integration

package businessstate_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationConcurrentRequirementBudgetsAndDuplicateCompletion(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state, _ := businessstate.NewRepository(db)
	environment, err := state.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := state.Budget(ctx, environment.DataEpoch)
	if err != nil {
		t.Fatal(err)
	}
	const payloadBytes = 128
	const charge = payloadBytes + businessstate.RetentionMetadataBytes
	historicalEpoch, err := bus.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	// 历史 epoch 仍占用全局保留预算，不能只统计当前 epoch。
	if _, err := db.ExecContext(ctx, "INSERT INTO business_async_budget(data_epoch,retained_groups,retained_bytes) VALUES(?,7,?)", historicalEpoch, 7*charge); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.Exec("DELETE FROM business_async_budget WHERE data_epoch=?", historicalEpoch) }()
	var globalGroups, globalBytes uint64
	if err := db.QueryRowContext(ctx, "SELECT SUM(retained_groups),SUM(retained_bytes) FROM business_async_budget").Scan(&globalGroups, &globalBytes); err != nil {
		t.Fatal(err)
	}
	limits := businessstate.DefaultBudgetLimits()
	for _, test := range []struct {
		name    string
		options []businessstate.RepositoryOption
	}{
		{"open_requirements", []businessstate.RepositoryOption{businessstate.WithOpenRequirementsLimit(baseline.OpenRequirements + 3)}},
		{"retained_groups", []businessstate.RepositoryOption{businessstate.WithRetentionLimits(globalGroups+3, limits.RetainedBytes)}},
		{"retained_bytes", []businessstate.RepositoryOption{businessstate.WithRetentionLimits(limits.RetainedGroups, globalBytes+3*charge)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			limited, err := businessstate.NewRepository(db, test.options...)
			if err != nil {
				t.Fatal(err)
			}
			const callers = 8
			requirements := make([]businessstate.Requirement, callers)
			for index := range requirements {
				id, err := bus.NewUUID()
				if err != nil {
					t.Fatal(err)
				}
				requirements[index] = businessstate.Requirement{DataEpoch: environment.DataEpoch, EventID: id,
					Consumer: businessstate.ConsumerSearch, RoutingContractRevision: businessstate.SearchRoutingContractV1,
					ApplicableObjectVersion: 1, PayloadBytes: payloadBytes}
			}
			defer func() {
				for _, requirement := range requirements {
					_, _ = db.Exec("DELETE FROM business_application_results WHERE data_epoch=? AND event_id=?", environment.DataEpoch, requirement.EventID)
					_, _ = db.Exec("DELETE FROM business_event_requirements WHERE data_epoch=? AND event_id=?", environment.DataEpoch, requirement.EventID)
				}
				_, _ = db.Exec("UPDATE business_async_budget SET open_requirements=?,retained_groups=?,retained_bytes=? WHERE data_epoch=?", baseline.OpenRequirements, baseline.RetainedGroups, baseline.RetainedBytes, environment.DataEpoch)
			}()
			start := make(chan struct{})
			results := make([]error, callers)
			var workers sync.WaitGroup
			for index, requirement := range requirements {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					results[index] = limited.RegisterRequirement(ctx, requirement)
				}()
			}
			close(start)
			workers.Wait()
			var accepted []businessstate.Requirement
			for index, err := range results {
				exists, readErr := state.HasRequirement(ctx, requirements[index])
				if readErr != nil || exists != (err == nil) {
					t.Fatalf("需求与事务结果不一致: exists=%v result=%v read=%v", exists, err, readErr)
				}
				if err == nil {
					accepted = append(accepted, requirements[index])
				} else if !errors.Is(err, businessstate.ErrBudgetExceeded) {
					t.Fatalf("并发登记失败: %v", err)
				}
			}
			if len(accepted) != 3 {
				t.Fatalf("预算只允许 3 条需求: accepted=%d", len(accepted))
			}
			// 预算已满时，重复登记仍须幂等，且不得重复扣账。
			if err := limited.RegisterRequirement(ctx, accepted[0]); err != nil {
				t.Fatalf("重复登记失败: %v", err)
			}
			budget, err := state.Budget(ctx, environment.DataEpoch)
			if err != nil || budget.OpenRequirements != baseline.OpenRequirements+3 || budget.RetainedGroups != baseline.RetainedGroups+3 || budget.RetainedBytes != baseline.RetainedBytes+3*charge {
				t.Fatalf("并发扣账不一致: budget=%+v baseline=%+v err=%v", budget, baseline, err)
			}
			completionErrors := make(chan error, len(accepted)*2)
			for _, requirement := range accepted {
				for range 2 {
					workers.Add(1)
					go func() {
						defer workers.Done()
						completionErrors <- limited.RecordApplication(ctx, businessstate.Application{
							DataEpoch: requirement.DataEpoch, EventID: requirement.EventID, Consumer: requirement.Consumer,
							RoutingContractRevision: requirement.RoutingContractRevision, ProjectionGeneration: "budget-concurrency-test",
							Result: "applied", ActualVersion: 1})
					}()
				}
			}
			workers.Wait()
			close(completionErrors)
			for err := range completionErrors {
				if err != nil {
					t.Fatalf("并发完成需求失败: %v", err)
				}
			}
			budget, err = state.Budget(ctx, environment.DataEpoch)
			if err != nil || budget.OpenRequirements != baseline.OpenRequirements || budget.RetainedGroups != baseline.RetainedGroups+3 || budget.RetainedBytes != baseline.RetainedBytes+3*charge {
				t.Fatalf("重复完成改变了预算: budget=%+v baseline=%+v err=%v", budget, baseline, err)
			}
		})
	}
}
