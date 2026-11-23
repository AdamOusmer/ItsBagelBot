// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/app/db/overlays/ent"
	"ItsBagelBot/app/db/overlays/ent/feedtotal"
	entoverlay "ItsBagelBot/app/db/overlays/ent/overlay"
	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/internal/domain/overlays/feed"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"
)

const (
	totalPrefix = "t."
	cheerPrefix = "b.cheer."
	giftPrefix  = "b.gift."
)

// FeedProfile is what the feed aggregator needs to know about a channel's overlays.
type FeedProfile struct {
	Owned    bool
	Subathon feed.SubathonRules
}

func (r *Overlays) FeedProfile(ctx context.Context, userID uint64) (FeedProfile, error) {
	rows, err := db.WithQuery(ctx, r.client.Overlay.Query().Where(entoverlay.UserID(userID)).All)
	if err != nil {
		return FeedProfile{}, err
	}
	profile := FeedProfile{Owned: len(rows) > 0}
	for _, row := range rows {
		if rules, found := subathonRules(row); found {
			profile.Subathon = rules
			break
		}
	}
	return profile, nil
}

func subathonRules(row *ent.Overlay) (feed.SubathonRules, bool) {
	var doc overlays.Document
	if row.Kind != entoverlay.KindCustom || codec.Unmarshal(row.Document, &doc) != nil {
		return feed.SubathonRules{}, false
	}
	for _, widget := range doc.Widgets {
		if rules, found := widgetRules(widget); found {
			return rules, true
		}
	}
	return feed.SubathonRules{}, false
}

func widgetRules(widget overlays.Widget) (feed.SubathonRules, bool) {
	var rules feed.SubathonRules
	raw, ok := widget.Settings["rules"]
	if widget.Kind != overlays.WidgetTimer || widget.Settings["mode"] != "subathon" || !ok {
		return rules, false
	}
	encoded, err := codec.Marshal(raw)
	if err != nil || codec.Unmarshal(encoded, &rules) != nil {
		return rules, false
	}
	return rules, rules.Valid()
}

// FeedTotals are a channel's durable all-time counts and leaders.
type FeedTotals struct {
	Counts     feed.Counts
	TopCheerer *feed.Leader
	TopGifter  *feed.Leader
}

// AddFeedTotals adds delta to the channel's durable counters; call it once per channel event.
func (r *Overlays) AddFeedTotals(ctx context.Context, userID uint64, delta feed.Delta) error {
	if len(delta.Counts) == 0 {
		return nil
	}
	steps := make([]db.CounterIncrement, 0, len(delta.Counts)+1)
	for _, count := range delta.Counts {
		steps = append(steps, db.CounterIncrement{Key: db.CounterKey{Owner: userID, Name: totalPrefix + count.Field}, Delta: count.Amount})
	}
	if delta.Leader != nil {
		steps = append(steps, db.CounterIncrement{Key: db.CounterKey{Owner: userID, Name: boardName(*delta.Leader)}, Delta: delta.Leader.Amount})
		if err := r.labelBoard(ctx, userID, *delta.Leader); err != nil {
			return err
		}
	}
	if err := r.ensureTotals(ctx, userID, steps); err != nil {
		return err
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.IncrementFeedTotals(ctx, steps)
		return err
	})
}

func boardName(leader feed.LeaderDelta) string {
	prefix := cheerPrefix
	if leader.Board == feed.BoardGift {
		prefix = giftPrefix
	}
	return prefix + leader.User.ID
}

func (r *Overlays) ensureTotals(ctx context.Context, userID uint64, steps []db.CounterIncrement) error {
	rows := make([]*ent.FeedTotalCreate, len(steps))
	for i, step := range steps {
		rows[i] = r.client.FeedTotal.Create().SetUserID(userID).SetName(step.Key.Name)
	}
	insert := r.client.FeedTotal.CreateBulk(rows...).OnConflictColumns(feedtotal.FieldUserID, feedtotal.FieldName).Ignore()
	return db.WithExec(ctx, insert.Exec)
}

func (r *Overlays) labelBoard(ctx context.Context, userID uint64, leader feed.LeaderDelta) error {
	label := leader.User.Name
	if label == "" {
		label = leader.User.Login
	}
	upsert := r.client.FeedTotal.Create().SetUserID(userID).SetName(boardName(leader)).SetLabel(label).
		OnConflictColumns(feedtotal.FieldUserID, feedtotal.FieldName).
		Update(func(u *ent.FeedTotalUpsert) { u.UpdateLabel() })
	return db.WithExec(ctx, upsert.Exec)
}

func (r *Overlays) FeedTotals(ctx context.Context, userID uint64) (FeedTotals, error) {
	rows, err := db.WithQuery(ctx, r.client.FeedTotal.Query().Where(feedtotal.UserID(userID), feedtotal.NameHasPrefix(totalPrefix)).All)
	if err != nil {
		return FeedTotals{}, err
	}
	var totals FeedTotals
	for _, row := range rows {
		totals.Counts.Add(feed.CountDelta{Field: strings.TrimPrefix(row.Name, totalPrefix), Amount: row.Total})
	}
	if totals.TopCheerer, err = r.topBoard(ctx, userID, cheerPrefix); err != nil {
		return FeedTotals{}, err
	}
	totals.TopGifter, err = r.topBoard(ctx, userID, giftPrefix)
	return totals, err
}

func (r *Overlays) topBoard(ctx context.Context, userID uint64, prefix string) (*feed.Leader, error) {
	row, err := db.WithQuery(ctx, r.client.FeedTotal.Query().
		Where(feedtotal.UserID(userID), feedtotal.NameHasPrefix(prefix), feedtotal.TotalGT(0)).
		Order(ent.Desc(feedtotal.FieldTotal), ent.Asc(feedtotal.FieldName)).First)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	leader := feed.Leader{User: feed.User{ID: strings.TrimPrefix(row.Name, prefix)}, Amount: row.Total}
	if row.Label != nil {
		leader.Name = *row.Label
	}
	return &leader, nil
}

func (r *Overlays) deleteFeedTotals(ctx context.Context, userID uint64, cutoff time.Time) error {
	_, err := db.WithQuery(ctx, r.client.FeedTotal.Delete().Where(feedtotal.UserID(userID), feedtotal.UpdatedAtLTE(cutoff.UTC().Truncate(time.Microsecond))).Exec)
	return err
}
