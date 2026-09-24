// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/channelfeedcounter"
	"ItsBagelBot/app/db/modules/ent/feedcounter"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

// feedCounterID is the fixed id of the single global feed-counter row.
const feedCounterID = 1

// feedBumpAttempts bounds the first-ever-bump race: two instances both miss
// the row and race the create; the loser hits the primary-key conflict and
// retries the update. After the row exists the loop always exits on the first
// pass.
const feedBumpAttempts = 3

var ErrFeedCountLimit = errors.New("feed counter reached its exact integer limit")

// FeedTotals is one feeding's persisted readout: the fleet-wide lifetime total
// (one bagel, every channel feeds it), this channel's own lifetime count, and
// where that count places the channel on the leaderboard (1 = fed the most).
// Rank is 0 when the bump carried no broadcaster, which is the only case where
// the channel half is not written.
type FeedTotals struct {
	Total   int64
	Channel int64
	Rank    uint64
}

// FeedBoardRow is one leaderboard entry: a channel, the name it carried at its
// last feeding, and its lifetime count.
type FeedBoardRow struct {
	BroadcasterID uint64
	Name          string
	Count         int64
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
func (p *Personality) FeedBump(ctx context.Context, broadcasterID uint64, name string, eventIDs ...string) (FeedTotals, error) {
	eventID := ""
	if len(eventIDs) > 0 {
		eventID = eventIDs[0]
	}
	if len(eventID) > 255 {
		return FeedTotals{}, errors.New("feed event identity too long")
	}
	return db.WithQuery(ctx, func(ctx context.Context) (FeedTotals, error) { return p.feedBump(ctx, broadcasterID, name, eventID) })
}

type feedRequest struct {
	broadcasterID uint64
	name          string
	eventID       string
}

func (p *Personality) feedBump(ctx context.Context, broadcasterID uint64, name, eventID string) (FeedTotals, error) {
	request := feedRequest{broadcasterID: broadcasterID, name: name, eventID: eventID}
	tx, err := p.client.Tx(ctx)
	if err != nil {
		return FeedTotals{}, err
	}
	defer func() { _ = tx.Rollback() }()
	write := &Personality{client: tx.Client()}
	if err := write.reserveFeed(ctx, eventID); err != nil {
		return p.replayedFeed(ctx, tx, eventID, err)
	}
	totals, err := write.incrementFeed(ctx, request)
	if err != nil {
		return FeedTotals{}, err
	}
	if err := tx.Commit(); err != nil {
		return FeedTotals{}, err
	}
	return p.rankFeed(ctx, broadcasterID, totals), nil
}

func (p *Personality) reserveFeed(ctx context.Context, eventID string) error {
	if eventID == "" {
		return nil
	}
	return p.client.FeedReceipt.Create().SetID(eventID).Exec(ctx)
}

func (p *Personality) replayedFeed(ctx context.Context, tx *ent.Tx, eventID string, err error) (FeedTotals, error) {
	if !ent.IsConstraintError(err) {
		return FeedTotals{}, err
	}
	if err := tx.Rollback(); err != nil {
		return FeedTotals{}, err
	}
	receipt, err := p.client.FeedReceipt.Get(ctx, eventID)
	if err != nil {
		return FeedTotals{}, err
	}
	return FeedTotals{Total: receipt.Total, Channel: receipt.Channel}, nil
}

func (p *Personality) incrementFeed(ctx context.Context, request feedRequest) (FeedTotals, error) {
	total, err := p.bumpGlobal(ctx)
	if err != nil {
		return FeedTotals{}, err
	}
	channel, err := p.bumpFeedChannel(ctx, request)
	if err != nil {
		return FeedTotals{}, err
	}
	totals := FeedTotals{Total: total, Channel: channel}
	return totals, p.completeFeedReceipt(ctx, request.eventID, totals)
}

func (p *Personality) bumpFeedChannel(ctx context.Context, request feedRequest) (int64, error) {
	if request.broadcasterID == 0 {
		return 0, nil
	}
	return p.bumpChannel(ctx, request.broadcasterID, request.name)
}

func (p *Personality) completeFeedReceipt(ctx context.Context, eventID string, totals FeedTotals) error {
	if eventID == "" {
		return nil
	}
	return p.client.FeedReceipt.UpdateOneID(eventID).SetTotal(totals.Total).SetChannel(totals.Channel).Exec(ctx)
}

func (p *Personality) rankFeed(ctx context.Context, broadcasterID uint64, totals FeedTotals) FeedTotals {
	if broadcasterID == 0 {
		return totals
	}
	// Rank is a readout after commit. A failed read must not cause the caller to
	// retry a feeding whose totals have already been persisted.
	rank, err := p.rankOf(ctx, totals.Channel)
	if err == nil {
		totals.Rank = rank
	}
	return totals
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
		if _, err := channelCountOf(row, nil); err != nil {
			return nil, err
		}
		board = append(board, FeedBoardRow{BroadcasterID: row.ID, Name: row.Name, Count: row.Count})
	}
	return board, nil
}

// FeedChannel reads one channel's lifetime count and rank without feeding the
// bagel; the leaderboard reaction uses it to say where the asking channel
// stands. A channel that never fed reads 0 and rank 0.
func (p *Personality) FeedChannel(ctx context.Context, broadcasterID uint64) (int64, uint64, error) {
	row, err := p.client.ChannelFeedCounter.Get(ctx, broadcasterID)
	if ent.IsNotFound(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	if _, err := channelCountOf(row, nil); err != nil {
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
func (p *Personality) FeedTotal(ctx context.Context) (int64, error) {
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
func (p *Personality) bumpGlobal(ctx context.Context) (int64, error) {
	return bumpRow(ctx,
		func(ctx context.Context) (int64, error) {
			row, err := p.client.FeedCounter.UpdateOneID(feedCounterID).
				Where(feedcounter.CountLT(data.MaxCounter)).AddCount(1).Save(ctx)
			err = feedUpdateError(err, func() error {
				_, err := p.client.FeedCounter.Get(ctx, feedCounterID)
				return err
			})
			return countOf(row, err)
		},
		func(ctx context.Context) (int64, error) {
			row, err := p.client.FeedCounter.Create().SetID(feedCounterID).SetCount(1).Save(ctx)
			return countOf(row, err)
		})
}

// bumpChannel increments one channel's row, refreshing the display name the
// leaderboard shows. An empty name leaves the stored one alone: a rename is
// worth following, a nameless event is not worth erasing it for.
func (p *Personality) bumpChannel(ctx context.Context, broadcasterID uint64, name string) (int64, error) {
	return bumpRow(ctx,
		func(ctx context.Context) (int64, error) {
			update := p.client.ChannelFeedCounter.UpdateOneID(broadcasterID).
				Where(channelfeedcounter.CountLT(data.MaxCounter)).AddCount(1)
			if name != "" {
				update = update.SetName(name)
			}
			row, err := update.Save(ctx)
			err = feedUpdateError(err, func() error {
				_, err := p.client.ChannelFeedCounter.Get(ctx, broadcasterID)
				return err
			})
			return channelCountOf(row, err)
		},
		func(ctx context.Context) (int64, error) {
			row, err := p.client.ChannelFeedCounter.Create().
				SetID(broadcasterID).SetCount(1).SetName(name).Save(ctx)
			return channelCountOf(row, err)
		})
}

// rankOf is the channel's leaderboard position: one more than the number of
// channels that have fed the bagel more often. Ties share the better rank.
func (p *Personality) rankOf(ctx context.Context, count int64) (uint64, error) {
	ahead, err := p.client.ChannelFeedCounter.Query().Where(channelfeedcounter.CountGT(count)).Count(ctx)
	if err != nil {
		return 0, err
	}
	return uint64(ahead) + 1, nil
}

// bumpRow runs the update-or-create dance both counters share: try the atomic
// increment, create the row when it is not there yet, and retry the update
// when another instance won the create race.
func bumpRow(ctx context.Context, update, create func(context.Context) (int64, error)) (int64, error) {
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

func countOf(row *ent.FeedCounter, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return validFeedCount(row.Count)
}

func channelCountOf(row *ent.ChannelFeedCounter, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return validFeedCount(row.Count)
}

// An update can miss because the row is absent or because its count is full.
func feedUpdateError(updateErr error, read func() error) error {
	if !ent.IsNotFound(updateErr) {
		return updateErr
	}
	err := read()
	if err == nil {
		return ErrFeedCountLimit
	}
	return err
}

func validFeedCount(count int64) (int64, error) {
	if count < 0 || count > data.MaxCounter {
		return 0, ErrFeedCountLimit
	}
	return count, nil
}
