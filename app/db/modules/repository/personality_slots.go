// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"hash/fnv"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/feedcounter"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

// Row 1 must remain untouched or rolling deployments and rollbacks lose the legacy total.
const feedSlots = 256

var errFeedSlotFull = errors.New("feed slot needs redistribution")

func feedSlot(request feedRequest) int {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(request.eventID))
	return int(hash.Sum64()%feedSlots) + 2
}

func (p *Personality) bumpGlobal(ctx context.Context, request feedRequest, rebalance bool) error {
	if rebalance {
		return p.redistributeFeed(ctx)
	}
	base, err := p.client.FeedCounter.Get(ctx, 1)
	if err != nil {
		return err
	}
	limit := (data.MaxCounter - base.Count) / feedSlots
	updated, err := p.client.FeedCounter.Update().
		Where(feedcounter.ID(feedSlot(request)), feedcounter.CountLT(limit)).AddCount(1).Save(ctx)
	if err != nil {
		return err
	}
	if updated == 0 {
		return errFeedSlotFull
	}
	return nil
}

func (p *Personality) FeedTotal(ctx context.Context) (int64, error) {
	var totals []int64
	err := p.client.FeedCounter.Query().Aggregate(ent.Sum(feedcounter.FieldCount)).Scan(ctx, &totals)
	if err != nil {
		return 0, err
	}
	return totals[0], nil
}

func (p *Personality) redistributeFeed(ctx context.Context) error {
	rows, err := p.lockFeedSlots(ctx)
	if err != nil {
		return err
	}
	total, err := feedSlotTotal(rows)
	if err != nil {
		return err
	}
	if total == data.MaxCounter {
		return ErrFeedCountLimit
	}
	return p.writeFeedSlots(ctx, total-rows[0].Count+1)
}

func (p *Personality) lockFeedSlots(ctx context.Context) ([]*ent.FeedCounter, error) {
	query := p.client.FeedCounter.Query().Order(ent.Asc(feedcounter.FieldID))
	rows, err := query.Clone().ForUpdate().All(ctx)
	if db.RowLocksUnsupported(err) {
		return query.All(ctx)
	}
	return rows, err
}

func feedSlotTotal(rows []*ent.FeedCounter) (int64, error) {
	var total int64
	for _, row := range rows {
		if row.Count > data.MaxCounter-total {
			return 0, ErrFeedCountLimit
		}
		total += row.Count
	}
	return total, nil
}

func (p *Personality) writeFeedSlots(ctx context.Context, total int64) error {
	rows := make([]*ent.FeedCounterCreate, feedSlots)
	for i := range rows {
		count := total / feedSlots
		if int64(i) < total%feedSlots {
			count++
		}
		rows[i] = p.client.FeedCounter.Create().SetID(i + 2).SetCount(count)
	}
	return p.client.FeedCounter.CreateBulk(rows...).OnConflict().UpdateNewValues().Exec(ctx)
}

func migrateFeedSlots(ctx context.Context, client *ent.Client) error {
	return db.RetryTx(ctx, func(ctx context.Context) error {
		return NewPersonality(client).initializeFeedSlots(ctx)
	})
}

func (p *Personality) initializeFeedSlots(ctx context.Context) error {
	ids, err := p.client.FeedCounter.Query().IDs(ctx)
	if err != nil {
		return err
	}
	existing := make(map[int]bool, len(ids))
	for _, id := range ids {
		existing[id] = true
	}
	var missing []*ent.FeedCounterCreate
	for id := 1; id <= feedSlots+1; id++ {
		if existing[id] {
			continue
		}
		missing = append(missing, p.client.FeedCounter.Create().SetID(id).SetCount(0))
	}
	if len(missing) == 0 {
		return nil
	}
	return p.client.FeedCounter.CreateBulk(missing...).OnConflict().Ignore().Exec(ctx)
}
