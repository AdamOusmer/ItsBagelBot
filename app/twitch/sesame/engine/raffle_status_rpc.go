// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/domain/rpc"
	drawrpc "ItsBagelBot/internal/domain/rpc/draw"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

type RaffleStatusRPC struct {
	Draw  DrawStore
	Watch LoyaltyTicker
	Log   *zap.Logger
}

func (s RaffleStatusRPC) Subscribe(w bus.RPCWiring) error {
	return bus.Serve(w, drawrpc.RaffleStatusSubject, s.status)
}

func (s RaffleStatusRPC) status(ctx context.Context, req drawrpc.RaffleStatusRequest) drawrpc.RaffleStatus {
	id, err := bus.UserID(req.UserID)
	if err != nil || id == 0 || req.Source != "dashboard" {
		return drawrpc.RaffleStatus{Refusal: rpc.Refused(rpc.CodeInvalid, "invalid user_id or source")}
	}
	rounds, err := s.Draw.Live(ctx, draw.ChannelID(id))
	if err != nil {
		s.Log.Warn("raffle status unavailable", zap.Error(err))
		return drawrpc.RaffleStatus{Refusal: rpc.Refused(rpc.CodeUnavailable, "raffle status unavailable")}
	}
	return drawrpc.RaffleStatus{AccrualHealth: string(s.Watch.AccrualHealth(ctx, id)), Unpaid: unpaidRaffleViewers(rounds)}
}

func unpaidRaffleViewers(rounds []draw.RoundView) []string {
	var names []string
	for _, round := range rounds {
		if round.Terms.Kind == draw.KindRaffle {
			names = appendUnpaidViewers(names, round.Slots)
		}
	}
	return names
}

func appendUnpaidViewers(names []string, slots []draw.Slot) []string {
	for _, slot := range slots {
		if slot.State == draw.SlotUnpaid && slot.Login != "" {
			names = append(names, slot.Login)
		}
	}
	return names
}
