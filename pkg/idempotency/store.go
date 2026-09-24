// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency

import (
	"context"
	"time"
)

// Seen must test-and-claim atomically on the primary, or a duplicate slips past a lagging replica.
type Store interface {
	Seen(ctx context.Context, key string, ttl time.Duration) (bool, error)

	Release(ctx context.Context, key string) error
}
