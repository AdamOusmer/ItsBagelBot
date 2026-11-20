// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/giveawaywinner"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/db"

	"entgo.io/ent/dialect/sql"
)

type GiveawayWinners struct {
	client *ent.Client
}

func NewGiveawayWinners(client *ent.Client) *GiveawayWinners {
	return &GiveawayWinners{client: client}
}

type WinnerRef struct {
	RoundID string
	Slot    uint8
}

type WinnerRow struct {
	WinnerRef
	ViewerID  uint64
	Login     string
	Prize     string
	WonAt     time.Time
	Claimed   bool
	ClaimedAt *time.Time
}

type WinnerDelivery struct {
	WinnerRef
	Delivered bool
	Note      string
}

func (g *GiveawayWinners) Record(ctx context.Context, userID uint64, rows []WinnerRow) error {
	if len(rows) == 0 {
		return nil
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		return db.WithTx(ctx, g.client, func(tx *ent.Tx) error {
			batch := tx.GiveawayWinner.MapCreateBulk(rows, func(create *ent.GiveawayWinnerCreate, i int) {
				row := rows[i]
				create.SetUserID(userID).SetRoundID(row.RoundID).SetSlot(row.Slot).
					SetViewerID(row.ViewerID).SetViewerLogin(row.Login).SetPrize(row.Prize).
					SetWonAt(row.WonAt.UTC()).SetClaimed(row.Claimed || row.ClaimedAt != nil).SetNillableClaimedAt(row.ClaimedAt)
			})
			return batch.OnConflictColumns(giveawaywinner.FieldUserID, giveawaywinner.FieldRoundID, giveawaywinner.FieldSlot).Ignore().Exec(ctx)
		})
	})
}

func (g *GiveawayWinners) MarkDelivered(ctx context.Context, userID uint64, delivery WinnerDelivery) (bool, error) {
	match := giveawaywinner.And(giveawaywinner.UserID(userID), giveawaywinner.RoundID(delivery.RoundID), giveawaywinner.Slot(delivery.Slot))
	update := g.client.GiveawayWinner.Update().Where(match)
	if delivery.Delivered {
		update.SetDeliveredAt(time.Now().UTC().Truncate(time.Millisecond)).SetDeliveryNote(delivery.Note)
	} else {
		update.ClearDeliveredAt().ClearDeliveryNote()
	}
	count, err := db.WithQuery(ctx, update.Save)
	if err != nil || count > 0 {
		return count > 0, err
	}
	return db.WithRead(ctx, g.client.GiveawayWinner.Query().Where(match).Exist)
}

func (g *GiveawayWinners) Recent(ctx context.Context, userID uint64, q modulesrpc.GiveawayRecentQuery) ([]modulesrpc.GiveawayWinner, error) {
	if q.Limit <= 0 {
		q.Limit = modulesrpc.GiveawayMaxRecentPage
	}
	query := g.client.GiveawayWinner.Query().Where(giveawaywinner.UserID(userID))
	if q.RoundID != "" {
		query.Where(giveawaywinner.RoundID(q.RoundID))
	}
	rows, err := db.WithRead(ctx, query.Order(giveawaywinner.ByWonAt(sql.OrderDesc()), giveawaywinner.ByID(sql.OrderDesc())).
		Limit(min(q.Limit, modulesrpc.GiveawayMaxRecentPage)).Offset(q.Offset).All)
	if err != nil {
		return nil, err
	}
	winners := make([]modulesrpc.GiveawayWinner, len(rows))
	for i, row := range rows {
		winners[i] = giveawayWinnerView(row)
	}
	return winners, nil
}

func giveawayWinnerView(row *ent.GiveawayWinner) modulesrpc.GiveawayWinner {
	view := modulesrpc.GiveawayWinner{
		RoundID: row.RoundID, Slot: int(row.Slot), Prize: row.Prize,
		ViewerID: strconv.FormatUint(row.ViewerID, 10), Login: row.ViewerLogin,
		WonAt: row.WonAt.UnixMilli(), Claimed: row.Claimed, DeliveryNote: row.DeliveryNote,
	}
	if row.ClaimedAt != nil {
		view.ClaimedAt = row.ClaimedAt.UnixMilli()
	}
	if row.DeliveredAt != nil {
		view.DeliveredAt = row.DeliveredAt.UnixMilli()
	}
	return view
}

func (g *GiveawayWinners) WonWithin(ctx context.Context, userID uint64, viewers []uint64, since time.Time) ([]uint64, error) {
	if len(viewers) == 0 {
		return nil, nil
	}
	return db.WithRead(ctx, func(ctx context.Context) ([]uint64, error) {
		var winners []uint64
		err := g.client.GiveawayWinner.Query().
			Where(giveawaywinner.UserID(userID), giveawaywinner.ViewerIDIn(viewers...), giveawaywinner.WonAtGTE(since.UTC())).
			Unique(true).Select(giveawaywinner.FieldViewerID).Scan(ctx, &winners)
		return winners, err
	})
}
