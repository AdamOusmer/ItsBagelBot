package ratelimit

import (
	"context"
	"time"
)

// Manager abstracts the rate limit decisions so the worker can remain
// ignorant of whether admission was granted centrally, locally, or via a peer.
type Manager interface {
	Allow(ctx context.Context, req Request) (bool, error)
	AllowOrdered(ctx context.Context, first, second Request) (uint8, error)
}

// GuardRetryAfter reports how long a manager expects its current lease
// generation guard to remain closed. Most managers have no lease guard and
// return zero through this helper. Control-plane callers may use the bounded
// hint to bridge the deliberately tiny epoch gap without mistaking it for real
// quota exhaustion.
func GuardRetryAfter(manager Manager) time.Duration {
	type guardReporter interface {
		GuardRetryAfter() time.Duration
	}
	reporter, ok := manager.(guardReporter)
	if !ok {
		return 0
	}
	return reporter.GuardRetryAfter()
}

// Ensure the existing Limiter implements the interface.
var _ Manager = (*Limiter)(nil)
