// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package monitor

import (
	"context"
	"sync/atomic"
)

type gateKey struct{}

type Gate struct {
	kept atomic.Bool
}

func Defer(ctx context.Context) (context.Context, *Gate) {
	gate := &Gate{}
	return context.WithValue(ctx, gateKey{}, gate), gate
}

func Keep(ctx context.Context) {
	if gate, ok := ctx.Value(gateKey{}).(*Gate); ok {
		gate.kept.Store(true)
	}
}

func (g *Gate) Kept() bool {
	return g.kept.Load()
}
