package ratelimit

import (
	"context"
)

// Manager abstracts the rate limit decisions so the worker can remain
// ignorant of whether admission was granted centrally, locally, or via a peer.
type Manager interface {
	Allow(ctx context.Context, req Request) (bool, error)
	AllowOrdered(ctx context.Context, first, second Request) (uint8, error)
}

// Ensure the existing Limiter implements the interface.
var _ Manager = (*Limiter)(nil)
