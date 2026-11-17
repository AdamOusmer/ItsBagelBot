// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"ItsBagelBot/pkg/bus"
)

type deliveryGroupKey struct {
	owner        any
	dependencies string
	failure      uint64
}

type deliveryGroup struct {
	batch *bus.CompletionBatch
	work  *Work
}

type Deliveries struct {
	mu       sync.Mutex
	groups   map[deliveryGroupKey]*deliveryGroup
	Retire   func()
	failures atomic.Uint64
}

func (d *Deliveries) FinishOnReturn(w *Work, c *bus.Completion, err *error) {
	if value := recover(); value != nil {
		d.finish(w, c, fmt.Errorf("effect panicked: %v", value))
		panic(value)
	}
	d.finish(w, c, *err)
}

func (d *Deliveries) finish(w *Work, c *bus.Completion, err error) {
	quarantined := c.EndExecution(err)
	if c.BatchOwner() != nil && !quarantined {
		dependencies, failure := w.transfer(err)
		d.compact(c, dependencies, failure)
		return
	}
	w.Watch(func(r Result) {
		if r.Final {
			c.Complete(r.Err)
			d.Retire()
		}
	})
	w.Seal(err)
}

func (w *Work) transfer(err error) ([]*Handoff, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.failure = err
	}
	dependencies := make([]*Handoff, 0, len(w.joined))
	for h, subscription := range w.joined {
		dependencies = append(dependencies, h)
		subscription.Cancel()
	}
	w.moved, w.joined = true, nil
	return dependencies, w.failure
}

func (d *Deliveries) compact(c *bus.Completion, dependencies []*Handoff, failure error) {
	if len(dependencies) == 0 {
		c.Complete(failure)
		d.Retire()
		return
	}
	key := deliveryGroupKey{owner: c.BatchOwner(), dependencies: dependencyKey(dependencies)}
	if failure != nil {
		key.failure = d.failures.Add(1)
	}
	d.mu.Lock()
	if d.groups == nil {
		d.groups = make(map[deliveryGroupKey]*deliveryGroup)
	}
	group, exists := d.groups[key]
	if !exists {
		group = d.newGroup(key, c, dependencies)
		d.groups[key] = group
	}
	compacted := c.Compact(group.batch)
	d.mu.Unlock()
	if !compacted {
		c.Complete(context.Canceled)
		d.Retire()
		d.forget(key, group)
	}
	if !exists {
		group.work.Seal(failure)
	}
}

func (d *Deliveries) newGroup(key deliveryGroupKey, c *bus.Completion, dependencies []*Handoff) *deliveryGroup {
	ctx, work := NewWork(context.Background())
	group := &deliveryGroup{work: work}
	group.batch = c.NewBatch(d.Retire, func() { d.forget(key, group) })
	for _, h := range dependencies {
		Join(ctx, h)
	}
	work.Watch(func(r Result) {
		if !r.Final {
			return
		}
		d.mu.Lock()
		d.removeGroup(key, group)
		d.mu.Unlock()
		group.batch.Complete(r.Err)
	})
	return group
}

func (d *Deliveries) forget(key deliveryGroupKey, group *deliveryGroup) {
	d.mu.Lock()
	if !group.batch.Empty() {
		d.mu.Unlock()
		return
	}
	d.removeGroup(key, group)
	d.mu.Unlock()
	group.work.transfer(nil)
}

func (d *Deliveries) removeGroup(key deliveryGroupKey, group *deliveryGroup) {
	if d.groups[key] == group {
		delete(d.groups, key)
	}
}

func dependencyKey(dependencies []*Handoff) string {
	ids := make([]uint64, 0, len(dependencies))
	for _, h := range dependencies {
		ids = append(ids, h.identity())
	}
	slices.Sort(ids)
	key := make([]byte, 0, len(ids)*8)
	for _, id := range ids {
		key = binary.LittleEndian.AppendUint64(key, id)
	}
	return string(key)
}
