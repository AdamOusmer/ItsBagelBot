// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "context"

type Reputation interface {
	Bump(ctx context.Context, chatterID string)
	Score(ctx context.Context, chatterID string) int
	Observe(context.Context, ReputationEvent) (int, error)
}

type ReputationEvent struct {
	Identity string
	Viewer   string
}

type NoopReputation struct{}

func (NoopReputation) Bump(context.Context, string)                          {}
func (NoopReputation) Score(context.Context, string) int                     { return 0 }
func (NoopReputation) Observe(context.Context, ReputationEvent) (int, error) { return 0, nil }
