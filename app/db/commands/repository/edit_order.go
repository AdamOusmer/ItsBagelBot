// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"math"
	"sync"

	"golang.org/x/sync/semaphore"
)

type editOrders struct {
	mu    sync.Mutex
	users map[uint64]*editOrder
}

type editOrder struct {
	gate *semaphore.Weighted
	refs int
}

func (o *editOrders) acquire(ctx context.Context, userID uint64, exclusive bool) (func(), error) {
	o.mu.Lock()
	if o.users == nil {
		o.users = make(map[uint64]*editOrder)
	}
	order := o.users[userID]
	if order == nil {
		order = &editOrder{gate: semaphore.NewWeighted(math.MaxInt64)}
		o.users[userID] = order
	}
	order.refs++
	o.mu.Unlock()
	weight := int64(1)
	if exclusive {
		weight = math.MaxInt64
	}
	if err := order.gate.Acquire(ctx, weight); err != nil {
		o.drop(userID, order)
		return nil, err
	}
	return func() { order.gate.Release(weight); o.drop(userID, order) }, nil
}

func (o *editOrders) drop(userID uint64, order *editOrder) {
	o.mu.Lock()
	defer o.mu.Unlock()
	order.refs--
	if order.refs == 0 {
		delete(o.users, userID)
	}
}
