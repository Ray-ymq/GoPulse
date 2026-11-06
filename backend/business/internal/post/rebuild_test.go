package post

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/resourcebudget"
)

func rebuildTestBody(key BodyKey) Body {
	return Body{ID: key.PostID, AuthorID: 7, ContentRevision: key.ContentRevision, Title: "title", Content: "content", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
}

func TestRebuildCoordinatorMergesSameKeyAndKeepsLeaderAfterCancellation(t *testing.T) {
	coordinator := NewRebuildCoordinator(context.Background(), RebuildOptions{
		MaxActiveKeys: 1, MaxExecutors: 1, MaxWaitersPerKey: 2, MaxRegisteredWaiters: 2,
		WaitTimeout: time.Second, TaskTimeout: time.Second,
	})
	key := BodyKey{Epoch: "epoch-a", PostID: 31, ContentRevision: 1}
	started := make(chan struct{})
	release := make(chan struct{})
	var loads atomic.Int32
	loader := func(ctx context.Context, got BodyKey) (Body, error) {
		loads.Add(1)
		close(started)
		select {
		case <-release:
			return rebuildTestBody(got), nil
		case <-ctx.Done():
			return Body{}, ctx.Err()
		}
	}
	firstContext, cancelFirst := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() {
		_, err := coordinator.Load(firstContext, key, loader)
		firstResult <- err
	}()
	<-started
	secondResult := make(chan error, 1)
	go func() {
		body, err := coordinator.Load(context.Background(), key, loader)
		if err == nil && body.ID != key.PostID {
			err = errors.New("unexpected body")
		}
		secondResult <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		_, _, registered := coordinator.State()
		if registered == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second caller did not register")
		}
		time.Sleep(time.Millisecond)
	}
	cancelFirst()
	if err := <-firstResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled leader result=%v", err)
	}
	close(release)
	if err := <-secondResult; err != nil {
		t.Fatalf("waiting result=%v", err)
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("loader calls=%d, want one", got)
	}
	active, executors, registered := coordinator.State()
	if active != 0 || executors != 0 || registered != 0 {
		t.Fatalf("coordinator state active=%d executors=%d registered=%d", active, executors, registered)
	}
}

func TestRebuildCoordinatorColdReadLoadsOneBodyForAllConcurrentCallers(t *testing.T) {
	coordinator := NewRebuildCoordinator(context.Background(), RebuildOptions{
		MaxActiveKeys: 1, MaxExecutors: 1, MaxWaitersPerKey: 16, MaxRegisteredWaiters: 16,
		WaitTimeout: time.Second, TaskTimeout: time.Second,
	})
	key := BodyKey{Epoch: "epoch-cold", PostID: 41, ContentRevision: 1}
	started := make(chan struct{})
	release := make(chan struct{})
	var loads atomic.Int32
	loader := func(ctx context.Context, got BodyKey) (Body, error) {
		loads.Add(1)
		close(started)
		select {
		case <-release:
			return rebuildTestBody(got), nil
		case <-ctx.Done():
			return Body{}, ctx.Err()
		}
	}

	const callers = 16
	results := make(chan error, callers)
	start := make(chan struct{})
	for index := 0; index < callers; index++ {
		go func() {
			<-start
			body, err := coordinator.Load(context.Background(), key, loader)
			if err == nil && !body.ValidForKey(key) {
				err = errors.New("loader returned an invalid body")
			}
			results <- err
		}()
	}
	close(start)
	<-started
	deadline := time.Now().Add(time.Second)
	for {
		_, _, registered := coordinator.State()
		if registered == callers {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("registered callers=%d, want %d", registered, callers)
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	for index := 0; index < callers; index++ {
		if err := <-results; err != nil {
			t.Fatalf("caller %d error=%v", index, err)
		}
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("cold body loads=%d, want one", got)
	}
	active, executors, registered := coordinator.State()
	if active != 0 || executors != 0 || registered != 0 {
		t.Fatalf("coordinator state active=%d executors=%d registered=%d", active, executors, registered)
	}
}

func TestRebuildCoordinatorReleasesAllCancelledWaitersAndResources(t *testing.T) {
	budget, err := resourcebudget.New(resourcebudget.Config{Auth: 1, Read: 1, Write: 1, Database: 1, Rebuild: 1, Waiter: 8})
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewRebuildCoordinator(context.Background(), RebuildOptions{
		MaxActiveKeys: 1, MaxExecutors: 1, MaxWaitersPerKey: 8, MaxRegisteredWaiters: 8,
		WaitTimeout: time.Second, TaskTimeout: time.Second, Budget: budget,
	})
	key := BodyKey{Epoch: "epoch-cancel", PostID: 42, ContentRevision: 1}
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context, got BodyKey) (Body, error) {
		close(started)
		select {
		case <-release:
			return rebuildTestBody(got), nil
		case <-ctx.Done():
			return Body{}, ctx.Err()
		}
	}

	const callers = 8
	contexts := make([]context.Context, callers)
	cancels := make([]context.CancelFunc, callers)
	results := make(chan error, callers)
	for index := 0; index < callers; index++ {
		contexts[index], cancels[index] = context.WithCancel(context.Background())
		go func(ctx context.Context) {
			_, loadErr := coordinator.Load(ctx, key, loader)
			results <- loadErr
		}(contexts[index])
	}
	<-started
	deadline := time.Now().Add(time.Second)
	for {
		_, _, registered := coordinator.State()
		if registered == callers {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("registered callers=%d, want %d", registered, callers)
		}
		time.Sleep(time.Millisecond)
	}
	for _, cancel := range cancels {
		cancel()
	}
	for index := 0; index < callers; index++ {
		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled caller %d error=%v", index, err)
		}
	}
	if budget.InFlight(resourcebudget.Rebuild) != 1 || budget.InFlight(resourcebudget.Database) != 1 || budget.InFlight(resourcebudget.Waiter) != callers {
		t.Fatalf("retained work before loader finish rebuild=%d database=%d waiter=%d", budget.InFlight(resourcebudget.Rebuild), budget.InFlight(resourcebudget.Database), budget.InFlight(resourcebudget.Waiter))
	}
	close(release)
	if err := coordinator.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if budget.InFlight(resourcebudget.Rebuild) != 0 || budget.InFlight(resourcebudget.Database) != 0 || budget.InFlight(resourcebudget.Waiter) != 0 {
		t.Fatalf("resources not released rebuild=%d database=%d waiter=%d", budget.InFlight(resourcebudget.Rebuild), budget.InFlight(resourcebudget.Database), budget.InFlight(resourcebudget.Waiter))
	}
}

func TestRebuildCoordinatorBoundsDifferentKeysAndRetainedWaiters(t *testing.T) {
	coordinator := NewRebuildCoordinator(context.Background(), RebuildOptions{
		MaxActiveKeys: 1, MaxExecutors: 1, MaxWaitersPerKey: 1, MaxRegisteredWaiters: 1,
		WaitTimeout: 50 * time.Millisecond, TaskTimeout: time.Second,
	})
	key := BodyKey{Epoch: "epoch-a", PostID: 31, ContentRevision: 1}
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = coordinator.Load(context.Background(), key, func(ctx context.Context, key BodyKey) (Body, error) {
			close(started)
			select {
			case <-release:
				return rebuildTestBody(key), nil
			case <-ctx.Done():
				return Body{}, ctx.Err()
			}
		})
	}()
	<-started
	other := BodyKey{Epoch: "epoch-a", PostID: 32, ContentRevision: 1}
	if _, err := coordinator.Load(context.Background(), other, func(context.Context, BodyKey) (Body, error) { return Body{}, nil }); !errors.Is(err, ErrRebuildBusy) {
		t.Fatalf("different key error=%v", err)
	}
	if _, err := coordinator.Load(context.Background(), key, func(context.Context, BodyKey) (Body, error) { return Body{}, nil }); !errors.Is(err, ErrRebuildBusy) {
		t.Fatalf("extra waiter error=%v", err)
	}
	close(release)
	if err := coordinator.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildCoordinatorUsesRebuildDatabaseAndWaiterBudgets(t *testing.T) {
	budget, err := resourcebudget.New(resourcebudget.Config{Auth: 1, Read: 1, Write: 1, Database: 1, Rebuild: 1, Waiter: 1})
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewRebuildCoordinator(context.Background(), RebuildOptions{
		MaxActiveKeys: 2, MaxExecutors: 2, MaxWaitersPerKey: 2, MaxRegisteredWaiters: 2,
		WaitTimeout: time.Second, TaskTimeout: time.Second, Budget: budget,
	})
	key := BodyKey{Epoch: "epoch-a", PostID: 31, ContentRevision: 1}
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, loadErr := coordinator.Load(context.Background(), key, func(ctx context.Context, got BodyKey) (Body, error) {
			close(started)
			select {
			case <-release:
				return rebuildTestBody(got), nil
			case <-ctx.Done():
				return Body{}, ctx.Err()
			}
		})
		result <- loadErr
	}()
	<-started
	if budget.InFlight(resourcebudget.Rebuild) != 1 || budget.InFlight(resourcebudget.Database) != 1 || budget.InFlight(resourcebudget.Waiter) != 1 {
		t.Fatalf("budgets rebuild=%d database=%d waiter=%d", budget.InFlight(resourcebudget.Rebuild), budget.InFlight(resourcebudget.Database), budget.InFlight(resourcebudget.Waiter))
	}
	if _, err := coordinator.Load(context.Background(), key, func(context.Context, BodyKey) (Body, error) { return Body{}, nil }); !errors.Is(err, ErrRebuildBusy) {
		t.Fatalf("second waiter error=%v", err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if budget.InFlight(resourcebudget.Rebuild) != 0 || budget.InFlight(resourcebudget.Database) != 0 || budget.InFlight(resourcebudget.Waiter) != 0 {
		t.Fatalf("budgets not released rebuild=%d database=%d waiter=%d", budget.InFlight(resourcebudget.Rebuild), budget.InFlight(resourcebudget.Database), budget.InFlight(resourcebudget.Waiter))
	}
}
