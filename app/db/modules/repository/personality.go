// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/channelfeedcounter"
)

// feedCounterID is the fixed id of the single global feed-counter row.
const feedCounterID = 1

// feedBumpAttempts bounds the first-ever-bump race: two instances both miss
// the row and race the create; the loser hits the primary-key conflict and
// retries the update. After the row exists the loop always exits on the first
// pass.
const feedBumpAttempts = 3

// FeedTotals is one feeding's persisted readout: the fleet-wide lifetime total
// (one bagel, every channel feeds it), this channel's own lifetime count, and
// where that count places the channel on the leaderboard (1 = fed the most).
// Rank is 0 when the bump carried no broadcaster, which is the only case where
// the channel half is not written.
type FeedTotals struct {
	Total   uint64
	Channel uint64
	Rank    uint64
}

// FeedBoardRow is one leaderboard entry: a channel, the name it carried at its
// last feeding, and its lifetime count.
type FeedBoardRow struct {
	BroadcasterID uint64
	Name          string
	Count         uint64
}

// Personality is the store behind the personality RPC verbs: the permanent
// fleet-wide "feed the bagel" counter and its per-channel breakdown. Both
// increments are atomic in SQL (count = count + 1) so concurrent bumps never
// lose a feeding.
type Personality struct {
	client *ent.Client
}

// NewPersonality returns the personality store.
func NewPersonality(client *ent.Client) *Personality {
	return &Personality{client: client}
}

// FeedBump increments the fleet-wide counter and, when the feeding names a
// channel, that channel's row too, returning both lifetime totals and the
// channel's standing. Rows are created on their first feeding.
func (p *Personality) FeedBump(ctx context.Context, broadcasterID uint64, name string) (FeedTotals, error) {
	total, err := p.bumpGlobal(ctx)
	if err != nil {
		return FeedTotals{}, err
	}
	if broadcasterID == 0 {
		return FeedTotals{Total: total}, nil
	}
	channel, err := p.bumpChannel(ctx, broadcasterID, name)
	if err != nil {
		return FeedTotals{}, err
	}
	rank, err := p.rankOf(ctx, channel)
	if err != nil {
		return FeedTotals{}, err
	}
	return FeedTotals{Total: total, Channel: channel, Rank: rank}, nil
}

// FeedBoard returns the channels that fed the bagel most, highest first. A
// limit of 0 or less returns every ranked channel, which is what sesame asks
// for when it seeds its live leaderboard view.
func (p *Personality) FeedBoard(ctx context.Context, limit int) ([]FeedBoardRow, error) {
	query := p.client.ChannelFeedCounter.Query().
		Order(ent.Desc(channelfeedcounter.FieldCount), ent.Asc(channelfeedcounter.FieldID))
	if limit > 0 {
		query = query.Limit(limit)
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	board := make([]FeedBoardRow, 0, len(rows))
	for _, row := range rows {
		board = append(board, FeedBoardRow{BroadcasterID: row.ID, Name: row.Name, Count: row.Count})
	}
	return board, nil
}

// FeedChannel reads one channel's lifetime count and rank without feeding the
// bagel; the leaderboard reaction uses it to say where the asking channel
// stands. A channel that never fed reads 0 and rank 0.
func (p *Personality) FeedChannel(ctx context.Context, broadcasterID uint64) (uint64, uint64, error) {
	row, err := p.client.ChannelFeedCounter.Get(ctx, broadcasterID)
	if ent.IsNotFound(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	rank, err := p.rankOf(ctx, row.Count)
	if err != nil {
		return 0, 0, err
	}
	return row.Count, rank, nil
}

// FeedTotal reads the fleet-wide lifetime count without feeding the bagel: the
// same number FeedBump returns, for readers (the public stats page) that must
// not have a side effect. A bagel nobody has fed yet reads 0.
func (p *Personality) FeedTotal(ctx context.Context) (uint64, error) {
	row, err := p.client.FeedCounter.Get(ctx, feedCounterID)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	return countOf(row, err)
}

// FeedRanked is how many channels have ever fed the bagel: the denominator of
// a "#3 of 57" standing.
func (p *Personality) FeedRanked(ctx context.Context) (uint64, error) {
	ranked, err := p.client.ChannelFeedCounter.Query().Count(ctx)
	if err != nil {
		return 0, err
	}
	return uint64(ranked), nil
}

// bumpGlobal increments the single fleet-wide row, creating it on the very
// first feeding.
func (p *Personality) bumpGlobal(ctx context.Context) (uint64, error) {
	return bumpRow(ctx,
		func(ctx context.Context) (uint64, error) {
			row, err := p.client.FeedCounter.UpdateOneID(feedCounterID).AddCount(1).Save(ctx)
			return countOf(row, err)
		},
		func(ctx context.Context) (uint64, error) {
			row, err := p.client.FeedCounter.Create().SetID(feedCounterID).SetCount(1).Save(ctx)
			return countOf(row, err)
		})
}

// bumpChannel increments one channel's row, refreshing the display name the
// leaderboard shows. An empty name leaves the stored one alone: a rename is
// worth following, a nameless event is not worth erasing it for.
func (p *Personality) bumpChannel(ctx context.Context, broadcasterID uint64, name string) (uint64, error) {
	return bumpRow(ctx,
		func(ctx context.Context) (uint64, error) {
			update := p.client.ChannelFeedCounter.UpdateOneID(broadcasterID).AddCount(1)
			if name != "" {
				update = update.SetName(name)
			}
			row, err := update.Save(ctx)
			return channelCountOf(row, err)
		},
		func(ctx context.Context) (uint64, error) {
			row, err := p.client.ChannelFeedCounter.Create().
				SetID(broadcasterID).SetCount(1).SetName(name).Save(ctx)
			return channelCountOf(row, err)
		})
}

// rankOf is the channel's leaderboard position: one more than the number of
// channels that have fed the bagel more often. Ties share the better rank.
func (p *Personality) rankOf(ctx context.Context, count uint64) (uint64, error) {
	ahead, err := p.client.ChannelFeedCounter.Query().Where(channelfeedcounter.CountGT(count)).Count(ctx)
	if err != nil {
		return 0, err
	}
	return uint64(ahead) + 1, nil
}

// bumpRow runs the update-or-create dance both counters share: try the atomic
// increment, create the row when it is not there yet, and retry the update
// when another instance won the create race.
func bumpRow(ctx context.Context, update, create func(context.Context) (uint64, error)) (uint64, error) {
	var lastErr error
	for range feedBumpAttempts {
		count, err := update(ctx)
		if err == nil {
			return count, nil
		}
		if !ent.IsNotFound(err) {
			return 0, err
		}
		count, err = create(ctx)
		if err == nil {
			return count, nil
		}
		if !ent.IsConstraintError(err) {
			return 0, err
		}
		lastErr = err // another instance created the row first; retry the update
	}
	return 0, fmt.Errorf("feed bump: creation contention: %w", lastErr)
}

func countOf(row *ent.FeedCounter, err error) (uint64, error) {
	if err != nil {
		return 0, err
	}
	return row.Count, nil
}

func channelCountOf(row *ent.ChannelFeedCounter, err error) (uint64, error) {
	if err != nil {
		return 0, err
	}
	return row.Count, nil
}
