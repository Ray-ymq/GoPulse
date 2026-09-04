package consumer

import (
	"context"
	"sync"
)

type Partition struct {
	Topic     string
	Partition int32
}
type Lease struct {
	generation uint64
	key        Partition
	ctx        context.Context
	owner      *Ownership
}

func (l Lease) Context() context.Context { return l.ctx }
func (l Lease) Valid() bool              { return l.owner != nil && l.owner.valid(l) }

type owned struct {
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
}
type Ownership struct {
	mu    sync.RWMutex
	next  uint64
	parts map[Partition]owned
}

func NewOwnership() *Ownership { return &Ownership{parts: map[Partition]owned{}} }
func (o *Ownership) Assign(partitions []Partition) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, p := range partitions {
		if old, ok := o.parts[p]; ok {
			old.cancel()
		}
		o.next++
		ctx, cancel := context.WithCancel(context.Background())
		o.parts[p] = owned{generation: o.next, ctx: ctx, cancel: cancel}
	}
}
func (o *Ownership) Revoke(partitions []Partition) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, p := range partitions {
		if old, ok := o.parts[p]; ok {
			old.cancel()
			delete(o.parts, p)
		}
	}
}
func (o *Ownership) Lose(partitions []Partition) { o.Revoke(partitions) }
func (o *Ownership) Lease(p Partition) (Lease, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	v, ok := o.parts[p]
	if !ok {
		return Lease{}, false
	}
	return Lease{generation: v.generation, key: p, ctx: v.ctx, owner: o}, true
}
func (o *Ownership) valid(l Lease) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	v, ok := o.parts[l.key]
	return ok && v.generation == l.generation && v.ctx.Err() == nil
}
func (o *Ownership) CancelAll() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for p, v := range o.parts {
		v.cancel()
		delete(o.parts, p)
	}
}
