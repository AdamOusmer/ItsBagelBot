// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ratelimit

import (
	"context"
	"time"
)

type Manager interface {
	Allow(ctx context.Context, req Request) (bool, error)
	AllowOrdered(ctx context.Context, first, second Request) (uint8, error)
}

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

var _ Manager = (*Limiter)(nil)
