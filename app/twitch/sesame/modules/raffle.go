// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"

	"go.uber.org/zap"
)

const raffleModuleName = "raffle"

const raffleWinnerCooldown = 5 * time.Second

const raffleDefaultMinutes = 10

func Raffle(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(raffleModuleName, module.KindOptIn)
	m.Command("raffle").Everyone().Run(raffleDispatch(d, log))
	m.Command("join").Everyone().Run(raffleStandalone(d, log, raffleCmd.join))
	m.Command("claim").Everyone().Run(raffleStandalone(d, log, raffleCmd.claimConfirm))
	m.Command("winner").Everyone().Cooldown(raffleWinnerCooldown).Run(raffleStandalone(d, log, raffleCmd.last))
	return m.Build()
}

type raffleConfig struct {
	OpenedMessage   string `json:"openedMessage"`
	JoinMessage     string `json:"joinMessage"`
	AlreadyMessage  string `json:"alreadyMessage"`
	NoRaffleMessage string `json:"noRaffleMessage"`
	WonMessage      string `json:"wonMessage"`
	ClaimOkMessage  string `json:"claimOkMessage"`
}

type raffleCmd struct {
	chatReplier
	r   engine.RaffleStore
	cfg raffleConfig
	log *zap.Logger
}

func newRaffleCmd(d engine.Deps, c *module.Context, log *zap.Logger) (rc raffleCmd, ok bool) {
	if d.Raffle == nil {
		return raffleCmd{}, false
	}
	rc = raffleCmd{chatReplier: newChatReplier(c), r: d.Raffle, log: log}
	_ = c.Decode(&rc.cfg)
	return rc, true
}

func raffleStandalone(d engine.Deps, log *zap.Logger, fn func(raffleCmd, context.Context, module.Emit) error) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		rc, ok := newRaffleCmd(d, c, log)
		if !ok {
			return nil
		}
		return fn(rc, ctx, emit)
	}
}

type raffleRoute struct {
	mod bool
	run func(rc raffleCmd, ctx context.Context, args string, emit module.Emit) error
}

var raffleRoutes = map[string]raffleRoute{
	"": {run: func(rc raffleCmd, ctx context.Context, _ string, emit module.Emit) error { return rc.status(ctx, emit) }},
	"open": {mod: true, run: func(rc raffleCmd, ctx context.Context, args string, emit module.Emit) error {
		return rc.open(ctx, args, emit)
	}},
	"draw": {mod: true, run: func(rc raffleCmd, ctx context.Context, args string, emit module.Emit) error {
		return rc.draw(ctx, args, emit)
	}},
	"close": {mod: true, run: func(rc raffleCmd, ctx context.Context, _ string, emit module.Emit) error {
		return rc.draw(ctx, "", emit)
	}},
	"cancel": {mod: true, run: func(rc raffleCmd, ctx context.Context, _ string, emit module.Emit) error { return rc.cancel(ctx, emit) }},
}

func raffleDispatch(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		rc, ok := newRaffleCmd(d, c, log)
		if !ok {
			return nil
		}
		sub, rest := splitFirst(args)
		route, known := raffleRoutes[strings.ToLower(sub)]
		if !known {
			rc.reply(emit, "", "raffle.err.usage")
			return nil
		}
		if route.mod && !c.Chatter().Allows(module.RoleModerator) {
			return nil
		}
		return route.run(rc, ctx, rest, emit)
	}
}

func (rc raffleCmd) status(ctx context.Context, emit module.Emit) error {
	st, err := rc.r.Status(ctx, rc.c.BroadcasterID)
	if err != nil {
		rc.log.Warn("raffle: status failed", rc.c.BID(), zap.Error(err))
		return err
	}
	if st.Open {
		rc.reply(emit, "", "raffle.status.open",
			"count", strconv.FormatInt(st.Entrants, 10),
			"mins", strconv.FormatInt((st.SecondsLeft+59)/60, 10))
	} else {
		rc.reply(emit, "", "raffle.status.closed")
	}
	return nil
}

func optInt(s string) (n int64, ok bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

func parseOpenArgs(args string) (minutes, winners int64, remind time.Duration) {
	minArg, rest := splitFirst(args)
	winArg, remRest := splitFirst(rest)
	remArg, _ := splitFirst(remRest)

	if n, ok := optInt(minArg); ok && n > 0 {
		minutes = n
	}
	if n, ok := optInt(winArg); ok && n > 0 {
		winners = n
	}
	if n, ok := optInt(remArg); ok {
		if n > 0 {
			remind = time.Duration(n) * time.Minute
		} else {
			remind = -time.Second
		}
	}
	return minutes, winners, remind
}

func (rc raffleCmd) open(ctx context.Context, args string, emit module.Emit) error {
	minutes, winners, remind := parseOpenArgs(args)

	ok, err := rc.r.Open(ctx, rc.c.BroadcasterID, engine.RaffleOpenSpec{
		OpenedBy: strings.ToLower(rc.c.Env.ChatterUserLogin),
		Winners:  winners,
		Duration: time.Duration(minutes) * time.Minute,
		Remind:   remind,
	})
	if err != nil {
		rc.log.Warn("raffle: open failed", rc.c.BID(), zap.Error(err))
		return err
	}
	if ok {
		rc.reply(emit, rc.cfg.OpenedMessage, "raffle.opened", "mins", strconv.FormatInt(minutes, 10))
	} else {
		rc.reply(emit, "", "raffle.open.already")
	}
	return nil
}

func (rc raffleCmd) join(ctx context.Context, emit module.Emit) error {
	login := strings.ToLower(rc.c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}
	entry, err := rc.r.Join(ctx, rc.c.BroadcasterID, login)
	if err != nil {
		rc.log.Warn("raffle: join failed", rc.c.BID(), zap.Error(err))
		return err
	}
	count := strconv.FormatInt(entry.Entrants, 10)
	switch {
	case !entry.Open:
		rc.reply(emit, rc.cfg.NoRaffleMessage, "raffle.join.closed")
	case entry.Joined:
		rc.reply(emit, rc.cfg.JoinMessage, "raffle.joined", "count", count)
	default:
		rc.reply(emit, rc.cfg.AlreadyMessage, "raffle.join.already", "count", count)
	}
	return nil
}

func (rc raffleCmd) draw(ctx context.Context, arg string, emit module.Emit) error {
	override := int64(0)
	if n, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64); err == nil && n > 0 {
		override = n
	}
	res, err := rc.r.Draw(ctx, rc.c.BroadcasterID, override)
	if err != nil {
		rc.log.Warn("raffle: draw failed", rc.c.BID(), zap.Error(err))
		return err
	}
	if res == nil {
		rc.reply(emit, "", "raffle.status.closed")
		return nil
	}
	rc.announceResult(emit, res)
	return nil
}

func (rc raffleCmd) cancel(ctx context.Context, emit module.Emit) error {
	ok, err := rc.r.Cancel(ctx, rc.c.BroadcasterID)
	if err != nil {
		rc.log.Warn("raffle: cancel failed", rc.c.BID(), zap.Error(err))
		return err
	}
	if ok {
		rc.reply(emit, "", "raffle.cancelled")
	} else {
		rc.reply(emit, "", "raffle.status.closed")
	}
	return nil
}

func (rc raffleCmd) last(ctx context.Context, emit module.Emit) error {
	res, found, err := rc.r.LastResult(ctx, rc.c.BroadcasterID)
	if err != nil {
		rc.log.Warn("raffle: last failed", rc.c.BID(), zap.Error(err))
		return err
	}
	switch {
	case !found:
		rc.reply(emit, "", "raffle.last.none")
	case len(res.Winners) == 0:
		rc.reply(emit, "", "raffle.last.empty")
	case len(res.Claims) > 0:
		rc.reply(emit, "", "raffle.last.confirmed",
			"targets", mentionTargets(res.Winners),
			"entrants", strconv.FormatInt(res.Entrants, 10),
			"confirmed", strconv.Itoa(len(res.Claims)),
			"total", strconv.Itoa(len(res.Winners)))
	default:
		rc.reply(emit, "", "raffle.last",
			"targets", mentionTargets(res.Winners),
			"entrants", strconv.FormatInt(res.Entrants, 10))
	}
	return nil
}

func (rc raffleCmd) claimConfirm(ctx context.Context, emit module.Emit) error {
	login := strings.ToLower(rc.c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}
	outcome, err := rc.r.Claim(ctx, rc.c.BroadcasterID, login)
	if err != nil {
		rc.log.Warn("raffle: claim failed", rc.c.BID(), zap.Error(err))
		return err
	}
	switch outcome {
	case engine.ClaimOk:
		rc.reply(emit, rc.cfg.ClaimOkMessage, "raffle.claim.ok")
	case engine.ClaimAlready:
		rc.reply(emit, "", "raffle.claim.already")
	case engine.ClaimLate:
		rc.reply(emit, "", "raffle.claim.late")
	default:
		rc.reply(emit, "", "raffle.claim.none")
	}
	return nil
}

func (rc raffleCmd) announceResult(emit module.Emit, res *engine.RaffleResult) {
	if len(res.Winners) == 0 {
		rc.reply(emit, "", "raffle.draw.empty")
		return
	}
	rc.reply(emit, rc.cfg.WonMessage, "raffle.won",
		"targets", mentionTargets(res.Winners),
		"count", strconv.Itoa(len(res.Winners)),
		"entrants", strconv.FormatInt(res.Entrants, 10))
}

func mentionTargets(winners []string) string {
	prefixed := make([]string, len(winners))
	for i, w := range winners {
		prefixed[i] = "@" + w
	}
	return strings.Join(prefixed, ", ")
}
