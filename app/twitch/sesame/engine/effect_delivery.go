// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"

	"ItsBagelBot/pkg/bus"
)

type effectDelivery struct {
	mu    sync.Mutex
	ctx   context.Context
	local bool
}

func (d *effectDelivery) sourceExpired() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.local || bus.DeliverySourceExpired(d.ctx)
}

func (d *effectDelivery) alive() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.local && bus.DeliveryAlive(d.ctx)
}

func (d *effectDelivery) compact() {
	d.mu.Lock()
	d.ctx, d.local = nil, true
	d.mu.Unlock()
}

func (d *effectDelivery) hold() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.local {
		bus.HoldDelivery(d.ctx)
	}
}
