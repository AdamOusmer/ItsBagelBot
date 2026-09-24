// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "context"

type Reputation interface {
	Bump(ctx context.Context, chatterID string)
	Score(ctx context.Context, chatterID string) int
}

type NoopReputation struct{}

func (NoopReputation) Bump(context.Context, string)      {}
func (NoopReputation) Score(context.Context, string) int { return 0 }
