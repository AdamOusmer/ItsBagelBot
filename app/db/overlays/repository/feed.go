// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	receiptPrefix = "r."

	FeedReceiptTTL = 24 * time.Hour
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

func subathonTimer(widget overlays.Widget) bool {
	return widget.Kind == overlays.WidgetTimer && widget.Settings["mode"] == "subathon"
}

func widgetRules(widget overlays.Widget) (feed.SubathonRules, bool) {
	var rules feed.SubathonRules
	raw, ok := widget.Settings["rules"]
	if !ok || !subathonTimer(widget) {
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

func (r *Overlays) AddFeedTotals(ctx context.Context, userID uint64, eventID string, delta feed.Delta) error {
	if len(delta.Counts) == 0 {
		return nil
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		return db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
			client := tx.Client()
			claimed, err := claimFeedEvent(ctx, client, userID, eventID)
			if err != nil || !claimed {
				return err
			}
			return addFeedTotals(ctx, client, userID, delta)
		})
	})
}

func (r *Overlays) PurgeFeedReceipts(ctx context.Context, userID uint64, before time.Time) error {
	_, err := db.WithQuery(ctx, r.client.FeedTotal.Delete().Where(
		feedtotal.UserID(userID), feedtotal.NameHasPrefix(receiptPrefix), feedtotal.UpdatedAtLT(before.UTC()),
	).Exec)
	return err
}

func claimFeedEvent(ctx context.Context, client *ent.Client, userID uint64, eventID string) (bool, error) {
	digest := sha256.Sum256([]byte(eventID))
	name := receiptPrefix + hex.EncodeToString(digest[:20])
	now := time.Now().UTC()
	insert := client.FeedTotal.Create().SetUserID(userID).SetName(name).OnConflictColumns(feedtotal.FieldUserID, feedtotal.FieldName).Ignore()
	if err := insert.Exec(ctx); err != nil {
		return false, err
	}
	claimed, err := client.FeedTotal.Update().Where(feedtotal.UserID(userID), feedtotal.Name(name), feedtotal.Total(0)).
		SetTotal(1).SetUpdatedAt(now).Save(ctx)
	return claimed == 1, err
}

func addFeedTotals(ctx context.Context, client *ent.Client, userID uint64, delta feed.Delta) error {
	steps := make([]db.CounterIncrement, 0, len(delta.Counts)+1)
	for _, count := range delta.Counts {
		steps = append(steps, db.CounterIncrement{Key: db.CounterKey{Owner: userID, Name: totalPrefix + count.Field}, Delta: count.Amount})
	}
	if delta.Leader != nil {
		steps = append(steps, db.CounterIncrement{Key: db.CounterKey{Owner: userID, Name: boardName(*delta.Leader)}, Delta: delta.Leader.Amount})
		if err := labelBoard(ctx, client, userID, *delta.Leader); err != nil {
			return err
		}
	}
	if err := ensureTotals(ctx, client, userID, steps); err != nil {
		return err
	}
	_, err := client.IncrementFeedTotals(ctx, steps)
	return err
}

func boardName(leader feed.LeaderDelta) string {
	prefix := cheerPrefix
	if leader.Board == feed.BoardGift {
		prefix = giftPrefix
	}
	return prefix + leader.User.ID
}

func ensureTotals(ctx context.Context, client *ent.Client, userID uint64, steps []db.CounterIncrement) error {
	rows := make([]*ent.FeedTotalCreate, len(steps))
	for i, step := range steps {
		rows[i] = client.FeedTotal.Create().SetUserID(userID).SetName(step.Key.Name)
	}
	return client.FeedTotal.CreateBulk(rows...).OnConflictColumns(feedtotal.FieldUserID, feedtotal.FieldName).Ignore().Exec(ctx)
}

func labelBoard(ctx context.Context, client *ent.Client, userID uint64, leader feed.LeaderDelta) error {
	label := leader.User.Name
	if label == "" {
		label = leader.User.Login
	}
	return client.FeedTotal.Create().SetUserID(userID).SetName(boardName(leader)).SetLabel(label).
		OnConflictColumns(feedtotal.FieldUserID, feedtotal.FieldName).
		Update(func(u *ent.FeedTotalUpsert) { u.UpdateLabel() }).Exec(ctx)
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
