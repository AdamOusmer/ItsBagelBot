// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"
)

func sweepEvery(ctx context.Context, interval time.Duration, sweep func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
