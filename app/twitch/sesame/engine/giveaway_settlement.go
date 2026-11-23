// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"go.uber.org/zap"
)

type GiveawayPhaseObserver struct {
	Snapshots StandingsSnapshots
	Log       *zap.Logger
}

func (o GiveawayPhaseObserver) LeftOpen(ctx context.Context, round draw.Round) {
	if round.Terms.Kind != draw.KindGiveaway || round.Terms.SnapshotID == "" {
		return
	}
	req := SnapshotDropRequest{Channel: round.Channel, SnapshotID: round.Terms.SnapshotID}
	if err := o.Snapshots.SnapshotDrop(ctx, req); err != nil {
		o.Log.Warn("giveaway: drop standings snapshot failed", zap.String("round_id", string(round.ID)), zap.Error(err))
	}
}

type GiveawayWinnerRecorder interface {
	Record(context.Context, uint64, []modulesrpc.GiveawayWinner) error
}

type GiveawayWinnerSink struct {
	Winners GiveawayWinnerRecorder
}

func (s GiveawayWinnerSink) Record(ctx context.Context, round draw.Round, slots []draw.Slot) error {
	var rows []modulesrpc.GiveawayWinner
	for _, slot := range slots {
		if settledGiveawayWinner(round, slot) {
			rows = append(rows, giveawayWinnerRow(round, slot))
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return s.Winners.Record(ctx, uint64(round.Channel), rows)
}

func settledGiveawayWinner(round draw.Round, slot draw.Slot) bool {
	switch slot.State {
	case draw.SlotClaimed:
		return true
	case draw.SlotWon:
		return round.Terms.ClaimWindow == 0
	default:
		return false
	}
}

func giveawayWinnerRow(round draw.Round, slot draw.Slot) modulesrpc.GiveawayWinner {
	wonAt := slot.WonAtMS
	if wonAt == 0 {
		wonAt = slot.Since.UnixMilli()
	}
	row := modulesrpc.GiveawayWinner{RoundID: string(round.ID), Slot: slot.N, ViewerID: string(slot.Viewer),
		Login: slot.Login, Prize: round.Terms.Prize.Item, WonAt: wonAt, Claimed: slot.State == draw.SlotClaimed}
	if !slot.ClaimedAt.IsZero() {
		row.ClaimedAt = slot.ClaimedAt.UnixMilli()
	}
	return row
}

func LastGiveawayWinners(ctx context.Context, store GiveawayWinnersStore, channel uint64) ([]modulesrpc.GiveawayWinner, error) {
	latest, err := store.Recent(ctx, channel, modulesrpc.GiveawayRecentQuery{Limit: 1})
	if err != nil || len(latest) == 0 {
		return nil, err
	}
	q := modulesrpc.GiveawayRecentQuery{RoundID: latest[0].RoundID, Limit: modulesrpc.GiveawayMaxRecentPage}
	var winners []modulesrpc.GiveawayWinner
	for {
		page, err := store.Recent(ctx, channel, q)
		if err != nil {
			return nil, err
		}
		winners = append(winners, page...)
		if len(page) < q.Limit || len(winners) >= draw.Pins.MaxWinners {
			return winners, nil
		}
		q.Offset += len(page)
	}
}
