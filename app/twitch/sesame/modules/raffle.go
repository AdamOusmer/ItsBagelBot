// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	catalog "ItsBagelBot/internal/domain/analytics"
)

const raffleModuleName = engine.RaffleModuleName

func Raffle(d engine.Deps) module.Module {
	m := module.NewModule(raffleModuleName, module.KindOptIn)
	m.Command("raffle").Everyone().Run(raffleHandler(d, raffleCommands.dispatch))
	m.Command("sraffle").Aliases("singleraffle").Everyone().Run(raffleHandler(d, raffleCommands.single))
	m.Command("mraffle").Aliases("multiraffle").Everyone().Run(raffleHandler(d, raffleCommands.multi))
	m.Command("cancelraffle").Everyone().Run(raffleHandler(d, raffleCommands.cancel))
	return m.Build()
}

type raffleCommands struct {
	chatReplier
	deps engine.Deps
	cfg  engine.RaffleModuleConfig
}

type raffleRunner func(raffleCommands, context.Context, string, module.Emit) error

func raffleHandler(d engine.Deps, run raffleRunner) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.Draw == nil {
			return nil
		}
		rc := raffleCommands{chatReplier: newChatReplier(c), deps: d}
		_ = c.Decode(&rc.cfg)
		return run(rc, ctx, args, emit)
	}
}

var raffleRoutes = map[string]raffleRunner{
	"status": raffleCommands.status,
	"draw":   raffleCommands.draw,
	"close":  raffleCommands.close,
	"cancel": raffleCommands.cancel,
	"join":   raffleCommands.join,
}

func (rc raffleCommands) dispatch(ctx context.Context, args string, emit module.Emit) error {
	sub, rest := splitFirst(args)
	if run := raffleRoutes[strings.ToLower(sub)]; run != nil {
		return run(rc, ctx, rest, emit)
	}
	if sub == "" {
		return rc.bare(ctx, emit)
	}
	return rc.single(ctx, args, emit)
}

func (rc raffleCommands) bare(ctx context.Context, emit module.Emit) error {
	switch {
	case !rc.isMod():
		return rc.status(ctx, "", emit)
	case rc.cfg.BareRaffle == "split":
		return rc.multi(ctx, "", emit)
	default:
		return rc.single(ctx, "", emit)
	}
}

func (rc raffleCommands) isMod() bool { return rc.c.Chatter().Allows(module.RoleModerator) }

func (rc raffleCommands) channel() draw.ChannelID { return draw.ChannelID(rc.c.BroadcasterID) }

func (rc raffleCommands) actor() draw.ViewerID { return draw.ViewerID(rc.c.Env.ChatterUserID) }

func (rc raffleCommands) single(ctx context.Context, args string, emit module.Emit) error {
	return rc.start(ctx, raffleStartInput{args: args, winners: draw.Pins.RaffleDefaultWinners}, emit)
}

func (rc raffleCommands) multi(ctx context.Context, args string, emit module.Emit) error {
	return rc.start(ctx, raffleStartInput{args: args, winners: draw.Pins.MultiRaffleDefaultWinners, multi: true}, emit)
}

func (rc raffleCommands) start(ctx context.Context, input raffleStartInput, emit module.Emit) error {
	if !rc.isMod() {
		return nil
	}
	req, valid := parseRaffleStart(input, rc.cfg)
	if !valid {
		rc.reply(emit, "", raffleUsageKey(input.args))
		return nil
	}
	round, refusal, err := engine.StartRaffle(ctx, rc.deps, draw.OpenRequest{
		Channel: rc.channel(), Actor: rc.actor(), Duration: req.duration,
		Terms: rc.cfg.Terms(engine.RaffleLaunch{Points: req.prize, Winners: req.winners, Multi: req.multi}),
	})
	if err != nil {
		return err
	}
	if refusal != "" {
		rc.reply(emit, "", raffleStartKeys[refusal])
		return nil
	}
	if rc.cfg.OpenedOn() {
		line := rc.cfg.OpenedLine(ctx, rc.deps.Proj, rc.c.Locale, round)
		rc.say(emit, line)
	}
	return nil
}

var raffleStartKeys = map[string]replyKey{
	"busy_giveaway": "draw.open.busy_giveaway",
	"busy_raffle":   "raffle.open.already",
	"loyalty_off":   "raffle.open.loyalty_off",
	"invalid":       "raffle.err.usage",
}

func (rc raffleCommands) status(ctx context.Context, _ string, emit module.Emit) error {
	status, err := rc.deps.RaffleStatus(ctx, rc.c.BroadcasterID)
	if err != nil {
		return err
	}
	if status.Open {
		rc.replyOpenStatus(emit, status)
		return nil
	}
	r, slots, settling, err := rc.deps.Draw.Settling(ctx, rc.channel(), draw.KindRaffle)
	if err != nil {
		return err
	}
	if settling {
		replyDrawWinner(rc.c, emit, r, slots)
		return nil
	}
	rc.reply(emit, "", "raffle.status.closed")
	return nil
}

func (rc raffleCommands) replyOpenStatus(emit module.Emit, status engine.RaffleStatus) {
	secs := status.SecondsLeft
	key := replyKey("raffle.status.open")
	if secs < 60 {
		key = "raffle.status.open.secs"
	}
	rc.reply(emit, "", key, "count", strconv.FormatInt(status.Entrants, 10),
		"secs", strconv.FormatInt(secs, 10), "mins", strconv.FormatInt((secs+59)/60, 10))
}

func (rc raffleCommands) draw(ctx context.Context, args string, emit module.Emit) error {
	if !rc.isMod() {
		return nil
	}
	p, ok := parseWinners(strings.TrimSpace(args), draw.WinnerPolicy{})
	if !ok {
		rc.reply(emit, "", "raffle.err.usage")
		return nil
	}
	var override *draw.WinnerPolicy
	if p != (draw.WinnerPolicy{}) {
		override = &p
	}
	return rc.closeRound(ctx, override, emit)
}

func (rc raffleCommands) close(ctx context.Context, _ string, emit module.Emit) error {
	if !rc.isMod() {
		return nil
	}
	return rc.closeRound(ctx, nil, emit)
}

func (rc raffleCommands) closeRound(ctx context.Context, winners *draw.WinnerPolicy, emit module.Emit) error {
	ok, err := rc.deps.Draw.Close(ctx, draw.CloseRequest{Channel: rc.channel(), Actor: rc.actor(), Winners: winners, Kind: draw.KindRaffle})
	if err != nil || ok {
		return err
	}
	drained, err := rc.drawLegacy(ctx, winners, emit)
	if err != nil || drained {
		return err
	}
	rounds, err := rc.deps.Draw.Live(ctx, rc.channel())
	if err != nil || slices.ContainsFunc(rounds, raffleDrawInProgress) {
		return err
	}
	rc.reply(emit, "", "raffle.status.closed")
	return nil
}

func raffleDrawInProgress(r draw.RoundView) bool {
	return r.Terms.Kind == draw.KindRaffle && (r.Phase == draw.PhaseClosing || r.Phase == draw.PhaseSettling)
}

func (rc raffleCommands) drawLegacy(ctx context.Context, winners *draw.WinnerPolicy, emit module.Emit) (bool, error) {
	if rc.deps.Raffle == nil {
		return false, nil
	}
	channel := rc.c.BroadcasterID
	status, err := rc.deps.Raffle.Status(ctx, channel)
	if err != nil || !status.Open {
		return false, err
	}
	var count int64
	if winners != nil {
		count = int64(draw.ResolveWinners(*winners, int(status.Entrants), draw.Prize{}))
	}
	res, err := rc.deps.Raffle.Draw(ctx, channel, count)
	if err != nil || res == nil {
		return true, err
	}
	currency := engine.DrawCurrencyName(ctx, rc.deps.Proj, channel)
	notice := engine.RaffleAnnouncement{
		Locale: string(rc.c.Locale), Points: currency,
		Config: engine.ReadLegacyRaffleConfig(ctx, rc.deps.Proj, channel),
	}
	for _, line := range notice.ResultLines(res) {
		rc.say(emit, line)
	}
	return true, nil
}

func (rc raffleCommands) cancel(ctx context.Context, _ string, emit module.Emit) error {
	if !rc.isMod() {
		return nil
	}
	round, ok, err := rc.deps.Draw.CancelRound(ctx, draw.ModAction{Channel: rc.channel(), Actor: rc.actor(), Kind: draw.KindRaffle})
	if err != nil {
		return err
	}
	if !ok {
		rc.reply(emit, "", "raffle.status.closed")
		return nil
	}
	rc.deps.Analytics.AddContext(ctx, catalog.GamesRounds, 1, engine.RaffleGame(round), "cancelled")
	rc.reply(emit, "", "raffle.cancelled")
	return nil
}

func (rc raffleCommands) join(ctx context.Context, _ string, emit module.Emit) error {
	r, open, err := rc.deps.Draw.Joinable(ctx, rc.channel())
	if err != nil {
		return err
	}
	owner := drawOwner{deps: rc.deps, log: rc.deps.Log}
	if open && r.Terms.Kind == draw.KindRaffle {
		return owner.joinRound(ctx, rc.c, r, emit)
	}
	if legacy, ok := owner.legacy(ctx, rc.c); ok && !open {
		return legacy.join(ctx, "", emit)
	}
	owner.replyRaffleJoin(ctx, rc.c, emit, draw.JoinOutcome{})
	return nil
}
