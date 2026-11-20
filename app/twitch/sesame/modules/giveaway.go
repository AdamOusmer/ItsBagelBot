// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
)

func Giveaway(d engine.Deps) module.Module {
	m := module.NewModule(engine.GiveawayModuleName, module.KindOptIn)
	m.Command("giveaway").Mod().Run(giveawayHandler(d, giveawayCommands.dispatch))
	m.Command("reroll").Mod().Run(giveawayHandler(d, giveawayCommands.reroll))
	return m.Build()
}

type giveawayCommands struct {
	chatReplier
	deps engine.Deps
}

type giveawayRun func(giveawayCommands, context.Context, string, module.Emit) error

func giveawayHandler(d engine.Deps, run giveawayRun) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		return run(giveawayCommands{chatReplier: newChatReplier(c), deps: d}, ctx, args, emit)
	}
}

var giveawayRoutes = map[string]giveawayRun{
	"":       giveawayCommands.status,
	"status": giveawayCommands.status,
	"start":  giveawayCommands.start,
	"draw":   giveawayCommands.close,
	"cancel": giveawayCommands.cancel,
}

func (g giveawayCommands) dispatch(ctx context.Context, args string, emit module.Emit) error {
	sub, rest := splitFirst(args)
	if run := giveawayRoutes[strings.ToLower(sub)]; run != nil {
		return run(g, ctx, rest, emit)
	}
	g.reply(emit, "", "giveaway.usage")
	return nil
}

func (g giveawayCommands) action() draw.ModAction {
	return draw.ModAction{Channel: draw.ChannelID(g.c.BroadcasterID), Actor: draw.ViewerID(g.c.Env.ChatterUserID), Kind: draw.KindGiveaway}
}

func (g giveawayCommands) start(ctx context.Context, args string, emit module.Emit) error {
	outcome, err := engine.StartGiveaway(ctx, g.deps, engine.GiveawayStart{
		Channel: g.c.BroadcasterID, Actor: g.action().Actor, ChatArgs: args,
	})
	if err != nil {
		return err
	}
	if outcome.Refusal != "" {
		g.reply(emit, "", replyKey(outcome.Refusal))
		return nil
	}
	if line := outcome.OpenedLine(g.c.Locale); line != "" {
		g.say(emit, line)
	}
	return nil
}

func (g giveawayCommands) close(ctx context.Context, _ string, emit module.Emit) error {
	target := g.action()
	applied, err := g.deps.Draw.Close(ctx, draw.CloseRequest{Channel: target.Channel, Actor: target.Actor, Kind: target.Kind})
	return g.actionReply(emit, applied, err, "")
}

func (g giveawayCommands) cancel(ctx context.Context, _ string, emit module.Emit) error {
	applied, err := g.deps.Draw.Cancel(ctx, g.action())
	return g.actionReply(emit, applied, err, "giveaway.cancelled")
}

func (g giveawayCommands) actionReply(emit module.Emit, applied bool, err error, key replyKey) error {
	switch {
	case err != nil:
		return err
	case !applied:
		g.reply(emit, "", "giveaway.action.unavailable")
	case key != "":
		g.reply(emit, "", key)
	}
	return nil
}

func (g giveawayCommands) reroll(ctx context.Context, args string, emit module.Emit) error {
	slot, ok := giveawayRerollSlot(args)
	if !ok {
		g.reply(emit, "", "giveaway.reroll.usage")
		return nil
	}
	target := g.action()
	changed, err := g.deps.Draw.Reroll(ctx, draw.SlotAction{Channel: target.Channel, Actor: target.Actor, Kind: target.Kind, Slot: slot})
	return g.actionReply(emit, changed > 0, err, "")
}

func giveawayRerollSlot(args string) (int, bool) {
	arg := strings.TrimSpace(args)
	if arg == "" {
		return 0, true
	}
	slot, err := strconv.Atoi(arg)
	if err != nil {
		return 0, false
	}
	return slot, slot >= 1 && slot <= draw.Pins.MaxWinners
}

func (g giveawayCommands) status(ctx context.Context, _ string, emit module.Emit) error {
	round, open, err := g.deps.Draw.Joinable(ctx, g.action().Channel)
	if err != nil {
		return err
	}
	if !open || round.Terms.Kind != draw.KindGiveaway {
		g.reply(emit, "", "giveaway.status.closed")
		return nil
	}
	left := i18n.CompactDuration(g.c.Locale, time.Until(round.ClosesAt))
	g.reply(emit, "", "giveaway.status.open", "prize", round.Terms.Prize.Item, "left", left)
	return nil
}

func giveawayVariables(d engine.Deps) variableRead {
	return func(ctx context.Context, c *module.Context) (map[string]string, error) {
		rounds, err := d.Draw.Live(ctx, draw.ChannelID(c.BroadcasterID))
		if err != nil {
			return nil, err
		}
		for _, r := range rounds {
			if values := activeGiveawayVariables(r); values != nil {
				return values, nil
			}
		}
		return nil, nil
	}
}

func activeGiveawayVariables(r draw.RoundView) map[string]string {
	left := time.Until(r.ClosesAt)
	if !activeGiveaway(r) || left <= 0 {
		return nil
	}
	seconds := int64((left + time.Second - 1) / time.Second)
	return map[string]string{"prize": r.Terms.Prize.Item, "count": strconv.Itoa(r.Entries), "left": strconv.FormatInt(seconds, 10)}
}

func activeGiveaway(r draw.RoundView) bool {
	return r.Terms.Kind == draw.KindGiveaway && r.Phase == draw.PhaseOpen
}
