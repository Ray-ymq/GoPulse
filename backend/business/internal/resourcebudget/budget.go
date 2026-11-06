package resourcebudget

import (
	"errors"
	"sync"
)

// Kind is a fixed resource vocabulary. It must never contain a route, user,
// source address, or other request-controlled label.
type Kind string

const (
	Auth     Kind = "auth"
	Read     Kind = "read"
	Write    Kind = "write"
	Database Kind = "database"
	Rebuild  Kind = "rebuild"
	Waiter   Kind = "waiter"
)

var ErrInvalidBudget = errors.New("invalid resource budget")

type Config struct {
	Auth     int
	Read     int
	Write    int
	Database int
	Rebuild  int
	Waiter   int
}

func (config Config) Validate() error {
	for _, value := range []int{config.Auth, config.Read, config.Write, config.Database, config.Rebuild, config.Waiter} {
		if value < 1 {
			return ErrInvalidBudget
		}
	}
	return nil
}

// Budget provides non-blocking admission for finite local capacity. A
// request that cannot acquire a slot must fail fast; there is no hidden queue.
type Budget struct {
	mu    sync.RWMutex
	slots map[Kind]chan struct{}
}

func New(config Config) (*Budget, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Budget{slots: map[Kind]chan struct{}{
		Auth: make(chan struct{}, config.Auth), Read: make(chan struct{}, config.Read), Write: make(chan struct{}, config.Write),
		Database: make(chan struct{}, config.Database), Rebuild: make(chan struct{}, config.Rebuild), Waiter: make(chan struct{}, config.Waiter),
	}}, nil
}

// TryAcquire returns a release function only when a slot is available.
func (budget *Budget) TryAcquire(kind Kind) (func(), bool) {
	if budget == nil {
		return func() {}, false
	}
	budget.mu.RLock()
	slots := budget.slots[kind]
	budget.mu.RUnlock()
	if slots == nil {
		return func() {}, false
	}
	select {
	case slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-slots }) }, true
	default:
		return func() {}, false
	}
}

func (budget *Budget) InFlight(kind Kind) int {
	if budget == nil {
		return 0
	}
	budget.mu.RLock()
	slots := budget.slots[kind]
	budget.mu.RUnlock()
	if slots == nil {
		return 0
	}
	return len(slots)
}
