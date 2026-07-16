package repository

import (
	"context"
	"fmt"

	"ItsBagelBot/app/modules/ent"
)

// feedCounterID is the fixed id of the single global feed-counter row.
const feedCounterID = 1

// feedBumpAttempts bounds the first-ever-bump race: two instances both miss
// the row and race the create; the loser hits the primary-key conflict and
// retries the update. After the row exists the loop always exits on the first
// pass.
const feedBumpAttempts = 3

// Personality is the store behind the personality RPC verbs. Today that is one
// thing: the permanent global "feed the bagel" counter, a single row whose
// increment is atomic in SQL (count = count + 1) so concurrent bumps never
// lose a feeding.
type Personality struct {
	client *ent.Client
}

// NewPersonality returns the personality store.
func NewPersonality(client *ent.Client) *Personality {
	return &Personality{client: client}
}

// FeedBump increments the global counter and returns the new lifetime total,
// creating the row on the very first feeding.
func (p *Personality) FeedBump(ctx context.Context) (uint64, error) {
	var lastErr error
	for range feedBumpAttempts {
		row, err := p.client.FeedCounter.UpdateOneID(feedCounterID).AddCount(1).Save(ctx)
		if err == nil {
			return row.Count, nil
		}
		if !ent.IsNotFound(err) {
			return 0, err
		}
		row, err = p.client.FeedCounter.Create().SetID(feedCounterID).SetCount(1).Save(ctx)
		if err == nil {
			return row.Count, nil
		}
		if !ent.IsConstraintError(err) {
			return 0, err
		}
		lastErr = err // another instance created the row first; retry the update
	}
	return 0, fmt.Errorf("feed bump: creation contention: %w", lastErr)
}
