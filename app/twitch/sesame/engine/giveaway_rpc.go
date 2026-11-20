// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/domain/rpc"
	drawrpc "ItsBagelBot/internal/domain/rpc/draw"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"

	"github.com/google/uuid"
)

const giveawayRPCTimeout = 10 * time.Second

type GiveawayBalanceCounter interface {
	BalanceCountAtLeast(context.Context, uint64, BalanceMinimums) (loyaltyrpc.BalanceCount, error)
}

type GiveawayRPC struct {
	Deps      Deps
	Announce  AnnouncementConfig
	Standings GiveawayBalanceCounter
}

type giveawayRPCInput struct {
	drawrpc.GiveawayRequest
	channel draw.ChannelID
}

func (in giveawayRPCInput) modAction() draw.ModAction {
	return draw.ModAction{Channel: in.channel, Actor: draw.ViewerID(in.ActorID), Round: draw.RoundID(in.RoundID), Kind: draw.KindGiveaway}
}

func (in giveawayRPCInput) slotAction() draw.SlotAction {
	return draw.SlotAction{Channel: in.channel, Actor: draw.ViewerID(in.ActorID), Round: draw.RoundID(in.RoundID), Kind: draw.KindGiveaway, Slot: in.Slot}
}

type giveawayRPCHandler func(context.Context, giveawayRPCInput) (drawrpc.GiveawayReply, error)

type giveawayRPCVerb struct {
	run      giveawayRPCHandler
	mutation bool
}

func (s GiveawayRPC) Subscribe(w bus.RPCWiring) error {
	routes := map[string]giveawayRPCVerb{
		drawrpc.VerbGiveawayLive:          {run: s.live},
		drawrpc.VerbGiveawayEligibleCount: {run: s.eligibleCount},
		drawrpc.VerbGiveawayStart:         {run: s.start, mutation: true},
		drawrpc.VerbGiveawayClose:         {run: s.roundAction(drawrpc.VerbGiveawayClose), mutation: true},
		drawrpc.VerbGiveawayExtend:        {run: s.roundAction(drawrpc.VerbGiveawayExtend), mutation: true},
		drawrpc.VerbGiveawayCancel:        {run: s.roundAction(drawrpc.VerbGiveawayCancel), mutation: true},
		drawrpc.VerbGiveawayReroll:        {run: s.slotAction(s.Deps.Draw.Reroll, false), mutation: true},
		drawrpc.VerbGiveawayEndSlots:      {run: s.slotAction(s.Deps.Draw.EndSlots, false), mutation: true},
		drawrpc.VerbGiveawaySkipRule:      {run: s.slotAction(s.Deps.Draw.SkipRule, true), mutation: true},
		drawrpc.VerbGiveawayMarkDelivered: {run: s.markDelivered, mutation: true},
	}
	verbs := make([]bus.Verb[drawrpc.GiveawayRequest, drawrpc.GiveawayReply], 0, len(routes))
	for name, route := range routes {
		verbs = append(verbs, bus.VerbForUser(name, route.handle))
	}
	return bus.ServeVerbs(w.Within(giveawayRPCTimeout), drawrpc.GiveawayPrefix, verbs...)
}

func (v giveawayRPCVerb) handle(ctx context.Context, req drawrpc.GiveawayRequest, userID uint64) (drawrpc.GiveawayReply, error) {
	switch {
	case userID == 0 || req.Source != "dashboard":
		return invalidGiveawayRPC("invalid user_id or source")
	case v.mutation && !validGiveawayActor(req):
		return invalidGiveawayRPC("invalid actor_id")
	default:
		return v.run(ctx, giveawayRPCInput{GiveawayRequest: req, channel: draw.ChannelID(userID)})
	}
}

func validGiveawayActor(req drawrpc.GiveawayRequest) bool {
	_, err := bus.NonZeroUserID(req.ActorID)
	return err == nil
}

func (s GiveawayRPC) start(ctx context.Context, in giveawayRPCInput) (drawrpc.GiveawayReply, error) {
	window := time.Duration(in.WindowSeconds) * time.Second
	if window/time.Second != time.Duration(in.WindowSeconds) {
		window = -time.Second
	}
	outcome, err := StartGiveaway(ctx, s.Deps, GiveawayStart{Channel: uint64(in.channel), Actor: draw.ViewerID(in.ActorID),
		Preset: in.Preset, Prize: in.Prize, Winners: in.Winners, Window: window})
	if err != nil {
		return drawrpc.GiveawayReply{}, err
	}
	if outcome.Refusal != "" {
		return giveawayLaunchRefusal(outcome.Refusal)
	}
	locale := s.locale(ctx, in.channel)
	s.Announce.Post(ctx, uint64(in.channel), outcome.OpenedLine(locale), s.Deps.Log)
	round := giveawayRoundWire(draw.RoundView{Round: outcome.Round}, giveawayRequirementLines(locale, outcome.Rules))
	return drawrpc.GiveawayReply{Done: true, Round: &round}, nil
}

func giveawayLaunchRefusal(reason string) (drawrpc.GiveawayReply, error) {
	code := rpc.CodeInvalid
	switch reason {
	case "giveaway.open.disabled", "giveaway.prize_blocked", "giveaway.open.loyalty_off":
		code = rpc.CodeForbidden
	case "giveaway.open.stalled", "giveaway.open.busy", "draw.open.busy_giveaway":
		code = rpc.CodeConflict
	}
	return drawrpc.GiveawayReply{Reason: reason, Refusal: rpc.Refused(code, reason)}, nil
}

type giveawayRoundVerb string

func (s GiveawayRPC) roundAction(verb giveawayRoundVerb) giveawayRPCHandler {
	return func(ctx context.Context, in giveawayRPCInput) (drawrpc.GiveawayReply, error) {
		if !validGiveawayRoundID(in) {
			return invalidGiveawayRPC("invalid round_id")
		}
		changed, err := s.applyRoundAction(ctx, in.modAction(), verb)
		return giveawayActionReply(changed, 0, err)
	}
}

func (s GiveawayRPC) applyRoundAction(ctx context.Context, target draw.ModAction, verb giveawayRoundVerb) (bool, error) {
	switch verb {
	case drawrpc.VerbGiveawayClose:
		return s.Deps.Draw.Close(ctx, draw.CloseRequest{Channel: target.Channel, Actor: target.Actor, Kind: target.Kind, Round: target.Round})
	case drawrpc.VerbGiveawayExtend:
		return s.Deps.Draw.Extend(ctx, draw.ExtendRequest{Channel: target.Channel, Actor: target.Actor, Kind: target.Kind, Round: target.Round})
	default:
		return s.Deps.Draw.Cancel(ctx, target)
	}
}

type giveawaySlotAction func(context.Context, draw.SlotAction) (int, error)

func (s GiveawayRPC) slotAction(run giveawaySlotAction, single bool) giveawayRPCHandler {
	return func(ctx context.Context, in giveawayRPCInput) (drawrpc.GiveawayReply, error) {
		if !validGiveawayRoundID(in) || !validGiveawaySlot(in.Slot, single) {
			return invalidGiveawayRPC("invalid round_id or slot")
		}
		changed, err := run(ctx, in.slotAction())
		return giveawayActionReply(changed > 0, changed, err)
	}
}

func (s GiveawayRPC) markDelivered(ctx context.Context, in giveawayRPCInput) (drawrpc.GiveawayReply, error) {
	if !validDelivery(in) {
		return invalidGiveawayRPC("invalid round_id, slot or delivered")
	}
	changed, err := s.Deps.GiveawayWinners.MarkDelivered(ctx, uint64(in.channel), modulesrpc.GiveawayDelivery{
		RoundID: in.RoundID, Slot: in.Slot, Delivered: *in.Delivered,
	})
	return giveawayActionReply(changed, 0, err)
}

func validDelivery(in giveawayRPCInput) bool {
	if in.Delivered == nil {
		return false
	}
	return validGiveawayRoundID(in) && validGiveawaySlot(in.Slot, true)
}

func (s GiveawayRPC) eligibleCount(ctx context.Context, in giveawayRPCInput) (drawrpc.GiveawayReply, error) {
	cfg, err := ReadGiveawayConfig(ctx, s.Deps.Proj, uint64(in.channel))
	if err != nil {
		return drawrpc.GiveawayReply{}, err
	}
	preset, ok := cfg.LaunchPreset(in.Preset)
	if !ok {
		return giveawayLaunchRefusal("giveaway.open.preset")
	}
	count, err := s.Standings.BalanceCountAtLeast(ctx, uint64(in.channel),
		BalanceMinimums{Points: preset.Rules.MinPoints, WatchSeconds: int64(preset.Rules.MinWatchSeconds)})
	return drawrpc.GiveawayReply{Count: &count.Count, FreezeID: count.FreezeID,
		FrozenAsOf: count.FrozenAsOf, ExpiresAt: count.ExpiresAt}, err
}

func validGiveawayRoundID(in giveawayRPCInput) bool {
	id, err := uuid.Parse(in.RoundID)
	return err == nil && in.RoundID == id.String()
}

func validGiveawaySlot(slot int, single bool) bool {
	if single && slot == 0 {
		return false
	}
	return slot >= 0 && slot <= draw.Pins.MaxWinners
}

func giveawayActionReply(applied bool, count int, err error) (drawrpc.GiveawayReply, error) {
	if err != nil {
		return drawrpc.GiveawayReply{}, err
	}
	if !applied {
		return drawrpc.GiveawayReply{Refusal: rpc.Refused(rpc.CodeConflict, "giveaway action no longer applies")}, nil
	}
	return drawrpc.GiveawayReply{Done: true, Affected: count}, nil
}

func invalidGiveawayRPC(message string) (drawrpc.GiveawayReply, error) {
	return drawrpc.GiveawayReply{Refusal: rpc.Refused(rpc.CodeInvalid, message)}, nil
}
