// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/channelfeedcounter"
)

const feedCounterID = 1

const feedBumpAttempts = 3

type FeedTotals struct {
	Total   uint64
	Channel uint64
	Rank    uint64
}

type FeedBoardRow struct {
	BroadcasterID uint64
	Name          string
	Count         uint64
}

type Personality struct {
	client *ent.Client
}

func NewPersonality(client *ent.Client) *Personality {
	return &Personality{client: client}
}

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

func (p *Personality) FeedTotal(ctx context.Context) (uint64, error) {
	row, err := p.client.FeedCounter.Get(ctx, feedCounterID)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	return countOf(row, err)
}

func (p *Personality) FeedRanked(ctx context.Context) (uint64, error) {
	ranked, err := p.client.ChannelFeedCounter.Query().Count(ctx)
	if err != nil {
		return 0, err
	}
	return uint64(ranked), nil
}

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

func (p *Personality) rankOf(ctx context.Context, count uint64) (uint64, error) {
	ahead, err := p.client.ChannelFeedCounter.Query().Where(channelfeedcounter.CountGT(count)).Count(ctx)
	if err != nil {
		return 0, err
	}
	return uint64(ahead) + 1, nil
}

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
		lastErr = err
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
