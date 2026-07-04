package engine

import "context"

// Reputation is the automod's per-chatter memory: a rolling strike count of how
// often a chatter has been actioned or caught in a duplicate flood recently. It
// backs the Tier-2 escalation (a repeat offender's timeout becomes a ban) and is
// fed by the folded-cohort fan-out. It is best-effort: a backend error never
// blocks a message.
type Reputation interface {
	Bump(ctx context.Context, chatterID string)
	Score(ctx context.Context, chatterID string) int
}

// NoopReputation disables the reputation signal (tests, or when no valkey is
// wired). Bump does nothing and Score is always 0.
type NoopReputation struct{}

func (NoopReputation) Bump(context.Context, string)      {}
func (NoopReputation) Score(context.Context, string) int { return 0 }
