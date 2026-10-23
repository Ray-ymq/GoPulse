package post

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/instrumentation"
	"github.com/Ray-ymq/GoPulse/backend/internal/resourcebudget"
)

var (
	ErrRebuildBusy        = errors.New("post body rebuild budget exhausted")
	ErrRebuildUnavailable = errors.New("post body rebuild unavailable")
)

type BodyLoader func(context.Context, BodyKey) (Body, error)

type RebuildOptions struct {
	MaxActiveKeys        int
	MaxExecutors         int
	MaxWaitersPerKey     int
	MaxRegisteredWaiters int
	WaitTimeout          time.Duration
	TaskTimeout          time.Duration
	Budget               *resourcebudget.Budget
}

type rebuildEntry struct {
	key            BodyKey
	done           chan struct{}
	body           Body
	err            error
	registered     int
	waiterReleases []func()
}

// RebuildCoordinator merges identical BodyKey loads in one process and keeps
// both the active key set and all retained registrations bounded. Cancelled
// callers leave their registration until the shared task finishes, so a hot
// key cannot grow unbounded through repeated join/cancel cycles.
type RebuildCoordinator struct {
	parent  context.Context
	cancel  context.CancelFunc
	options RebuildOptions

	mu                sync.Mutex
	entries           map[string]*rebuildEntry
	activeKeys        int
	registeredWaiters int
	activeWaiters     int
	activeExecutors   chan struct{}
	wg                sync.WaitGroup
}

func NewRebuildCoordinator(parent context.Context, options ...RebuildOptions) *RebuildCoordinator {
	if parent == nil {
		parent = context.Background()
	}
	config := RebuildOptions{
		MaxActiveKeys:        64,
		MaxExecutors:         8,
		MaxWaitersPerKey:     64,
		MaxRegisteredWaiters: 256,
		WaitTimeout:          1500 * time.Millisecond,
		TaskTimeout:          2 * time.Second,
	}
	if len(options) > 0 {
		provided := options[0]
		if provided.MaxActiveKeys > 0 {
			config.MaxActiveKeys = provided.MaxActiveKeys
		}
		if provided.MaxExecutors > 0 {
			config.MaxExecutors = provided.MaxExecutors
		}
		if provided.MaxWaitersPerKey > 0 {
			config.MaxWaitersPerKey = provided.MaxWaitersPerKey
		}
		if provided.MaxRegisteredWaiters > 0 {
			config.MaxRegisteredWaiters = provided.MaxRegisteredWaiters
		}
		if provided.WaitTimeout > 0 {
			config.WaitTimeout = provided.WaitTimeout
		}
		if provided.TaskTimeout > 0 {
			config.TaskTimeout = provided.TaskTimeout
		}
		config.Budget = provided.Budget
	}
	ctx, cancel := context.WithCancel(parent)
	return &RebuildCoordinator{
		parent: ctx, cancel: cancel, options: config,
		entries: make(map[string]*rebuildEntry), activeExecutors: make(chan struct{}, config.MaxExecutors),
	}
}

func (coordinator *RebuildCoordinator) Load(ctx context.Context, key BodyKey, loader BodyLoader) (Body, error) {
	if coordinator == nil || loader == nil || !key.Valid() {
		return Body{}, ErrRebuildUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	entry, leader, err := coordinator.register(key)
	if err != nil {
		return Body{}, err
	}
	defer coordinator.leaveWaiter()
	if leader {
		coordinator.wg.Add(1)
		go coordinator.run(entry, loader)
	}
	waitContext := ctx
	var cancel context.CancelFunc
	if coordinator.options.WaitTimeout > 0 {
		waitContext, cancel = context.WithTimeout(ctx, coordinator.options.WaitTimeout)
		defer cancel()
	}
	select {
	case <-entry.done:
		return entry.body, entry.err
	case <-waitContext.Done():
		// The entry remains registered and the leader continues independently.
		return Body{}, waitContext.Err()
	}
}

func (coordinator *RebuildCoordinator) register(key BodyKey) (*rebuildEntry, bool, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	entryKey := key.String()
	if entry := coordinator.entries[entryKey]; entry != nil {
		if entry.registered >= coordinator.options.MaxWaitersPerKey || coordinator.registeredWaiters >= coordinator.options.MaxRegisteredWaiters {
			return nil, false, ErrRebuildBusy
		}
		release, ok := coordinator.acquireWaiter()
		if !ok {
			return nil, false, ErrRebuildBusy
		}
		entry.registered++
		entry.waiterReleases = append(entry.waiterReleases, release)
		coordinator.registeredWaiters++
		coordinator.activeWaiters++
		coordinator.observeState()
		return entry, false, nil
	}
	if coordinator.activeKeys >= coordinator.options.MaxActiveKeys || coordinator.registeredWaiters >= coordinator.options.MaxRegisteredWaiters {
		return nil, false, ErrRebuildBusy
	}
	release, ok := coordinator.acquireWaiter()
	if !ok {
		return nil, false, ErrRebuildBusy
	}
	entry := &rebuildEntry{key: key, done: make(chan struct{}), registered: 1, waiterReleases: []func(){release}}
	coordinator.entries[entryKey] = entry
	coordinator.activeKeys++
	coordinator.registeredWaiters++
	coordinator.activeWaiters++
	coordinator.observeState()
	return entry, true, nil
}

func (coordinator *RebuildCoordinator) run(entry *rebuildEntry, loader BodyLoader) {
	defer coordinator.wg.Done()
	taskContext, cancel := context.WithTimeout(coordinator.parent, coordinator.options.TaskTimeout)
	defer cancel()
	select {
	case coordinator.activeExecutors <- struct{}{}:
		defer func() { <-coordinator.activeExecutors }()
	case <-taskContext.Done():
		entry.err = taskContext.Err()
		coordinator.finish(entry)
		return
	}
	if coordinator.options.Budget != nil {
		release, ok := coordinator.options.Budget.TryAcquire(resourcebudget.Rebuild)
		if !ok {
			entry.err = ErrRebuildBusy
			coordinator.finish(entry)
			return
		}
		defer release()
		databaseRelease, databaseOK := coordinator.options.Budget.TryAcquire(resourcebudget.Database)
		if !databaseOK {
			entry.err = ErrRebuildBusy
			coordinator.finish(entry)
			return
		}
		defer databaseRelease()
	}
	entry.body, entry.err = loader(taskContext, entry.key)
	coordinator.finish(entry)
}

func (coordinator *RebuildCoordinator) finish(entry *rebuildEntry) {
	var releases []func()
	coordinator.mu.Lock()
	delete(coordinator.entries, entry.key.String())
	coordinator.activeKeys--
	coordinator.registeredWaiters -= entry.registered
	if coordinator.registeredWaiters < 0 {
		coordinator.registeredWaiters = 0
	}
	releases = entry.waiterReleases
	entry.waiterReleases = nil
	coordinator.observeState()
	for _, release := range releases {
		release()
	}
	close(entry.done)
	coordinator.mu.Unlock()
}

func (coordinator *RebuildCoordinator) acquireWaiter() (func(), bool) {
	if coordinator.options.Budget == nil {
		return func() {}, true
	}
	return coordinator.options.Budget.TryAcquire(resourcebudget.Waiter)
}

func (coordinator *RebuildCoordinator) leaveWaiter() {
	coordinator.mu.Lock()
	if coordinator.activeWaiters > 0 {
		coordinator.activeWaiters--
	}
	coordinator.observeState()
	coordinator.mu.Unlock()
}

func (coordinator *RebuildCoordinator) observeState() {
	if metrics := instrumentation.BackendActive(); metrics != nil {
		metrics.ObserveRebuildState(coordinator.activeKeys, coordinator.activeWaiters, coordinator.registeredWaiters)
	}
}

// Stop cancels shared tasks and waits for them to release their retained
// registrations. It is called during service drain before an epoch switch.
func (coordinator *RebuildCoordinator) Stop(ctx context.Context) error {
	if coordinator == nil {
		return nil
	}
	coordinator.cancel()
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	go func() {
		coordinator.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (coordinator *RebuildCoordinator) State() (activeKeys, activeExecutors, registeredWaiters int) {
	if coordinator == nil {
		return 0, 0, 0
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	return coordinator.activeKeys, len(coordinator.activeExecutors), coordinator.registeredWaiters
}
