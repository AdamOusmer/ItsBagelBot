// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/channelfeedcounter"
	"ItsBagelBot/app/db/modules/ent/feedreceipt"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

var ErrFeedCountLimit = errors.New("feed counter reached its integer limit")

var errFeedReplay = errors.New("feed receipt already committed")

type FeedTotals struct {
	Total   int64
	Channel int64
	Rank    uint64
}

type FeedBoardRow struct {
	BroadcasterID uint64
	Name          string
	Count         int64
}

type Personality struct {
	client *ent.Client
}

func NewPersonality(client *ent.Client) *Personality {
	return &Personality{client: client}
}

func (p *Personality) FeedBump(ctx context.Context, broadcasterID uint64, name string, eventIDs ...string) (FeedTotals, error) {
	eventID := ""
	if len(eventIDs) > 0 {
		eventID = eventIDs[0]
	}
	if eventID == "" || len(eventID) > 255 {
		return FeedTotals{}, fmt.Errorf("%w: feed event identity", ErrInvalidInput)
	}
	request := feedRequest{broadcasterID: broadcasterID, name: name, eventID: eventID}
	return db.WithQuery(ctx, func(ctx context.Context) (FeedTotals, error) { return p.feedBump(ctx, request) })
}

type feedRequest struct {
	broadcasterID uint64
	name          string
	eventID       string
}

func (p *Personality) feedBump(ctx context.Context, request feedRequest) (FeedTotals, error) {
	var totals FeedTotals
	rebalance := false
	err := db.RetryTx(ctx, func(ctx context.Context) error {
		var err error
		totals, err = db.WithTxResult(ctx, p.client, func(tx *ent.Tx) (FeedTotals, error) {
			write := &Personality{client: tx.Client()}
			return write.incrementFeed(ctx, request, rebalance)
		})
		rebalance = rebalance || errors.Is(err, errFeedSlotFull)
		return err
	}, errFeedSlotFull)
	if errors.Is(err, errFeedReplay) {
		return p.replayedFeed(ctx, request)
	}
	if err != nil {
		return FeedTotals{}, err
	}
	totals, err = p.finishFeed(ctx, request, totals)
	if err != nil {
		return FeedTotals{}, err
	}
	return p.rankFeed(ctx, request.broadcasterID, totals), nil
}

func (p *Personality) replayedFeed(ctx context.Context, request feedRequest) (FeedTotals, error) {
	receipt, err := p.client.FeedReceipt.Get(ctx, request.eventID)
	if err != nil {
		return FeedTotals{}, err
	}
	totals := FeedTotals{Total: receipt.Total, Channel: receipt.Channel}
	if totals.Total == 0 {
		totals, err = p.finishFeed(ctx, request, totals)
	}
	return totals, err
}

func (p *Personality) incrementFeed(ctx context.Context, request feedRequest, rebalance bool) (FeedTotals, error) {
	if err := p.client.FeedReceipt.Create().SetID(request.eventID).Exec(ctx); err != nil {
		return FeedTotals{}, feedReceiptError(err)
	}
	if err := p.bumpGlobal(ctx, request, rebalance); err != nil {
		return FeedTotals{}, err
	}
	channel, err := p.bumpFeedChannel(ctx, request)
	if err != nil {
		return FeedTotals{}, err
	}
	err = p.client.FeedReceipt.Update().Where(feedreceipt.ID(request.eventID)).SetChannel(channel).Exec(ctx)
	return FeedTotals{Channel: channel}, err
}

func feedReceiptError(err error) error {
	if ent.IsConstraintError(err) {
		return errors.Join(errFeedReplay, err)
	}
	return err
}

func (p *Personality) bumpFeedChannel(ctx context.Context, request feedRequest) (int64, error) {
	if request.broadcasterID == 0 {
		return 0, nil
	}
	return p.bumpChannel(ctx, request)
}

func (p *Personality) finishFeed(ctx context.Context, request feedRequest, totals FeedTotals) (FeedTotals, error) {
	total, err := p.FeedTotal(ctx)
	if err != nil {
		return FeedTotals{}, err
	}
	updated, err := p.client.FeedReceipt.Update().
		Where(feedreceipt.ID(request.eventID), feedreceipt.TotalEQ(0)).SetTotal(total).Save(ctx)
	if err != nil {
		return FeedTotals{}, err
	}
	if updated == 0 {
		return p.replayedFeed(ctx, request)
	}
	totals.Total = total
	return totals, nil
}

func (p *Personality) rankFeed(ctx context.Context, broadcasterID uint64, totals FeedTotals) FeedTotals {
	if broadcasterID == 0 {
		return totals
	}
	rank, err := p.rankOf(ctx, totals.Channel)
	if err == nil {
		totals.Rank = rank
	}
	return totals
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

func (p *Personality) FeedChannel(ctx context.Context, broadcasterID uint64) (int64, uint64, error) {
	row, err := p.client.ChannelFeedCounter.Get(ctx, broadcasterID)
	if ent.IsNotFound(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	rank, err := p.rankOf(ctx, row.Count)
	return row.Count, rank, err
}

func (p *Personality) FeedRanked(ctx context.Context) (uint64, error) {
	ranked, err := p.client.ChannelFeedCounter.Query().Count(ctx)
	if err != nil {
		return 0, err
	}
	return uint64(ranked), nil
}

func (p *Personality) bumpChannel(ctx context.Context, request feedRequest) (int64, error) {
	err := p.client.ChannelFeedCounter.Create().SetID(request.broadcasterID).
		SetName(request.name).OnConflict().Ignore().Exec(ctx)
	if err != nil {
		return 0, err
	}
	update := p.client.ChannelFeedCounter.UpdateOneID(request.broadcasterID).
		Where(channelfeedcounter.CountLT(data.MaxCounter)).AddCount(1)
	if request.name != "" {
		update = update.SetName(request.name)
	}
	row, err := update.Save(ctx)
	if ent.IsNotFound(err) {
		return 0, ErrFeedCountLimit
	}
	if err != nil {
		return 0, err
	}
	return row.Count, nil
}

func (p *Personality) rankOf(ctx context.Context, count int64) (uint64, error) {
	ahead, err := p.client.ChannelFeedCounter.Query().Where(channelfeedcounter.CountGT(count)).Count(ctx)
	if err != nil {
		return 0, err
	}
	return uint64(ahead) + 1, nil
}
