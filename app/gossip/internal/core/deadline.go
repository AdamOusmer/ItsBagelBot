// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"time"
)

const replyBudget = 100 * time.Millisecond

type fetchDeadlineKey struct{}

// WithFetchTimeout bounds upstream work separately from a caller's wait. Shared
// cache fills retain this deadline even when that caller cancels or times out.
func WithFetchTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(timeout)
	if parent, ok := ctx.Value(fetchDeadlineKey{}).(time.Time); ok && parent.Before(deadline) {
		deadline = parent
	}
	return context.WithDeadline(context.WithValue(ctx, fetchDeadlineKey{}, deadline), deadline)
}

// FetchContext leaves time to shape and publish a timeout reply before the
// enclosing RPC expires. Short requests reserve a proportion of their budget.
func FetchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 {
		return context.WithCancel(ctx)
	}
	reserve := min(replyBudget, time.Until(deadline)/10)
	return context.WithDeadline(ctx, deadline.Add(-reserve))
}
