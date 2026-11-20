// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/rpc"
	drawrpc "ItsBagelBot/internal/domain/rpc/draw"
	"ItsBagelBot/pkg/bus"

	"github.com/google/uuid"
)

type RaffleRPC struct {
	Deps     Deps
	Announce AnnouncementConfig
}

type raffleRPCInput struct {
	drawrpc.RaffleRequest
	channel draw.ChannelID
}

type raffleRPCHandler func(context.Context, raffleRPCInput) (drawrpc.RaffleReply, error)

func (s RaffleRPC) Subscribe(w bus.RPCWiring) error {
	verbs := []bus.Verb[drawrpc.RaffleRequest, drawrpc.RaffleReply]{
		bus.VerbForUser(drawrpc.VerbRaffleStart, raffleRPCHandler(s.start).handle),
		bus.VerbForUser(drawrpc.VerbRaffleDraw, s.action(drawrpc.VerbRaffleDraw).handle),
		bus.VerbForUser(drawrpc.VerbRaffleCancel, s.action(drawrpc.VerbRaffleCancel).handle),
	}
	return bus.ServeVerbs(w.Within(giveawayRPCTimeout), drawrpc.RafflePrefix, verbs...)
}

func (h raffleRPCHandler) handle(ctx context.Context, req drawrpc.RaffleRequest, user uint64) (drawrpc.RaffleReply, error) {
	_, err := bus.NonZeroUserID(req.ActorID)
	if err != nil || user == 0 {
		return raffleRPCRefusal("invalid"), nil
	}
	return h(ctx, raffleRPCInput{RaffleRequest: req, channel: draw.ChannelID(user)})
}

func (s RaffleRPC) start(ctx context.Context, in raffleRPCInput) (drawrpc.RaffleReply, error) {
	if !in.validStart() {
		return raffleRPCRefusal("invalid"), nil
	}
	cfg := ReadRaffleConfig(ctx, s.Deps.Proj, uint64(in.channel))
	r, refusal, err := StartRaffle(ctx, s.Deps, in.openRequest(cfg))
	if err != nil || refusal != "" {
		return raffleRPCRefusal(refusal), err
	}
	s.announceOpen(ctx, cfg, r)
	auto := r.Terms.Winners.Auto
	return drawrpc.RaffleReply{RoundID: string(r.ID), ClosesMS: r.ClosesAt.UnixMilli(),
		Winners: draw.ResolveWinners(r.Terms.Winners, 0, r.Terms.Prize), Auto: &auto, Amount: r.Terms.Prize.Points}, nil
}

func (s RaffleRPC) announceOpen(ctx context.Context, cfg RaffleModuleConfig, r draw.Round) {
	if !cfg.OpenedOn() {
		return
	}
	ch := uint64(r.Channel)
	user, _ := s.Deps.Proj.User(ctx, ch)
	s.Announce.Post(ctx, ch, cfg.OpenedLine(ctx, s.Deps.Proj, user.Locale, r), s.Deps.Log)
}

func (in raffleRPCInput) validStart() bool {
	switch {
	case in.Amount < 0 || in.Amount > MaxRafflePrize:
		return false
	case in.Seconds < 0 || in.Seconds > int(maxRaffleDuration/time.Second):
		return false
	case in.Seconds > 0 && in.Seconds < int(minRaffleDuration/time.Second):
		return false
	default:
		return validRaffleWinners(in.Winners)
	}
}

func (in raffleRPCInput) openRequest(cfg RaffleModuleConfig) draw.OpenRequest {
	duration, points := cfg.Duration(), cfg.Prize()
	if in.Seconds != 0 {
		duration = time.Duration(in.Seconds) * time.Second
	}
	if in.Amount != 0 {
		points = in.Amount
	}
	return draw.OpenRequest{Channel: in.channel, Actor: draw.ViewerID(in.ActorID), Duration: duration,
		Terms: cfg.Terms(RaffleLaunch{Points: points, Winners: in.startWinners(), Multi: in.Split})}
}

func (in raffleRPCInput) startWinners() draw.WinnerPolicy {
	switch {
	case !in.Split:
		return draw.WinnerPolicy{Count: draw.Pins.RaffleDefaultWinners}
	case in.Winners != 0:
		return draw.WinnerPolicy{Count: in.Winners, Auto: in.Auto}
	default:
		return draw.WinnerPolicy{Count: draw.Pins.MultiRaffleDefaultWinners, Auto: in.Auto}
	}
}

type raffleVerb string

func (s RaffleRPC) action(verb raffleVerb) raffleRPCHandler {
	return func(ctx context.Context, in raffleRPCInput) (drawrpc.RaffleReply, error) {
		if !in.validAction() {
			return raffleRPCRefusal("invalid"), nil
		}
		applied, err := s.apply(ctx, in, verb)
		if err != nil {
			return drawrpc.RaffleReply{}, err
		}
		if !applied {
			return raffleRPCRefusal("invalid"), nil
		}
		return drawrpc.RaffleReply{Done: true}, nil
	}
}

func (in raffleRPCInput) validAction() bool {
	return canonicalRaffleRoundID(draw.RoundID(in.RoundID)) && validRaffleWinners(in.Winners)
}

func canonicalRaffleRoundID(raw draw.RoundID) bool {
	id, err := uuid.Parse(string(raw))
	return err == nil && raw == draw.RoundID(id.String())
}

func validRaffleWinners(n int) bool { return n >= 0 && n <= draw.Pins.MaxWinners }

func (s RaffleRPC) apply(ctx context.Context, in raffleRPCInput, verb raffleVerb) (bool, error) {
	target := draw.ModAction{Channel: in.channel, Actor: draw.ViewerID(in.ActorID), Round: draw.RoundID(in.RoundID), Kind: draw.KindRaffle}
	if verb == drawrpc.VerbRaffleCancel {
		return s.cancel(ctx, target)
	}
	return s.Deps.Draw.Close(ctx, draw.CloseRequest{Channel: target.Channel, Actor: target.Actor, Round: target.Round,
		Kind: target.Kind, Winners: in.drawWinners()})
}

func (s RaffleRPC) cancel(ctx context.Context, target draw.ModAction) (bool, error) {
	round, applied, err := s.Deps.Draw.CancelRound(ctx, target)
	if applied {
		s.Deps.Analytics.AddContext(ctx, catalog.GamesRounds, 1, RaffleGame(round), "cancelled")
	}
	return applied, err
}

func (in raffleRPCInput) drawWinners() *draw.WinnerPolicy {
	if in.Winners == 0 && !in.Auto {
		return nil
	}
	return &draw.WinnerPolicy{Count: in.Winners, Auto: in.Auto}
}

func raffleRPCRefusal(reason string) drawrpc.RaffleReply {
	code := rpc.CodeInvalid
	switch reason {
	case "":
		return drawrpc.RaffleReply{}
	case "busy_giveaway", "busy_raffle":
		code = rpc.CodeConflict
	case "loyalty_off":
		code = rpc.CodeForbidden
	}
	return drawrpc.RaffleReply{Reason: reason, Refusal: rpc.Refused(code, reason)}
}
