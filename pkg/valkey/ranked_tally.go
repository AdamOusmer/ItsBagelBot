// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// RankedTally is a sorted set of member totals, highest first.
type RankedTally struct {
	Key string
	TTL time.Duration
}

// RankedEntry is one member and its total.
type RankedEntry struct {
	Member string
	Total  int64
}

// Add adds delta to member and renews the set TTL.
func (r RankedTally) Add(ctx context.Context, client valkey_go.Client, member string, delta int64) error {
	for _, result := range client.DoMulti(ctx,
		client.B().Zincrby().Key(r.Key).Increment(float64(delta)).Member(member).Build(),
		client.B().Expire().Key(r.Key).Seconds(int64(r.TTL.Seconds())).Build(),
	) {
		if err := result.Error(); err != nil {
			return err
		}
	}
	return nil
}

// Totals returns each member's total in order; an absent member counts 0.
func (r RankedTally) Totals(ctx context.Context, client valkey_go.Client, members []string) ([]int64, error) {
	commands := make(valkey_go.Commands, len(members))
	for i, member := range members {
		commands[i] = client.B().Zscore().Key(r.Key).Member(member).Build()
	}
	totals := make([]int64, len(members))
	for i, result := range client.DoMulti(ctx, commands...) {
		score, err := result.AsFloat64()
		if err != nil && !valkey_go.IsValkeyNil(err) {
			return nil, err
		}
		totals[i] = int64(score)
	}
	return totals, nil
}

// Top returns the n highest members, highest first.
func (r RankedTally) Top(ctx context.Context, client valkey_go.Client, n int64) ([]RankedEntry, error) {
	return r.rank(ctx, client, n-1)
}

// All returns every member, highest first.
func (r RankedTally) All(ctx context.Context, client valkey_go.Client) ([]RankedEntry, error) {
	return r.rank(ctx, client, -1)
}

func (r RankedTally) rank(ctx context.Context, client valkey_go.Client, stop int64) ([]RankedEntry, error) {
	scores, err := client.Do(ctx, client.B().Zrevrange().Key(r.Key).Start(0).Stop(stop).Withscores().Build()).AsZScores()
	if err != nil {
		return nil, err
	}
	out := make([]RankedEntry, len(scores))
	for i, score := range scores {
		out[i] = RankedEntry{Member: score.Member, Total: int64(score.Score)}
	}
	return out, nil
}
