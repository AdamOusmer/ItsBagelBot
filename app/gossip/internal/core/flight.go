// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"errors"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"
)

var errSharedFetchPanic = errors.New("gossip shared fetch panicked")

func (c *Cache) load(ctx context.Context, key string, fetch func(context.Context) (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = c.fillContext(ctx, key)
	result := c.sf.DoChan(flightKey(ctx), func() (value any, err error) {
		defer func() {
			if recover() != nil {
				value, err = nil, errSharedFetchPanic
			}
		}()
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), swrRefreshTimeout)
		defer cancel()
		if deadline, ok := ctx.Value(fetchDeadlineKey{}).(time.Time); ok {
			bounded, finish := context.WithDeadline(work, deadline)
			defer finish()
			work = bounded
		}
		work = newrelic.NewContext(work, newrelic.FromContext(ctx).NewGoroutine())
		return fetch(work)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case out := <-result:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		RecordFetchError(ctx, out.Err)
		return out.Val, out.Err
	}
}
