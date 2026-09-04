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
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// raffleModuleName is the ModuleView key; the console MODULE_CATALOG entry and
// the dashboard module page use the same id.
const raffleModuleName = "raffle"

// raffleWinnerCooldown throttles !winner per channel: a receipt read is one
// cheap valkey op, but the reply names users, so a spam window keeps chat
// clean without dropping anyone's raffle entry (joins stay uncooled).
const raffleWinnerCooldown = 5 * time.Second

// raffleDefaultMinutes for a bare "!raffle open". Winner and reminder
// defaults, plus every ceiling, live store-side in clampRaffleOpen.
const raffleDefaultMinutes = 10

// Raffle owns the channel raffle: a timed pool viewers join with !join that
// closes itself on its deadline and draws winners uniformly at random. It is
// a named, opt-in module (KindOptIn): off by default, enabled on the
// dashboard.
//
//	!raffle                  → status (open/closed, entries, time left)
//	!raffle open [min] [n] [r] → mod: start a raffle; r is the reminder cadence
//	                           in minutes (default 5, 0 = off). The bot posts a
//	                           time-left line each tick and draws automatically
//	                           when the deadline runs out.
//	!raffle draw [n]         → mod: close now and announce n winners
//	!raffle close            → mod: same as draw with the configured count
//	!raffle cancel           → mod: tear down without drawing
//	!join                    → enter the running raffle
//	!claim                   → winners confirm their prize inside the window
//	!winner                  → recall the last draw's winners and claims
//
// This module registers BEFORE the queue module in All() and owns !join:
// when a channel runs both features, the raffle wins the standalone spelling,
// and the queue stays reachable through !queue join / !queue leave.
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

// raffleConfig holds the broadcaster's customized reply templates for the
// viewer-facing conversational lines. Each field is a dashboard-editable
// message; empty falls back to that reply's localized default (see i18n, keyed
// by the constant next to each field). Only the conversational replies are
// customizable — the status readout, moderator-action confirmations, claim
// outcomes beyond the first confirmation and the usage/error lines stay fixed
// system text, so their keys have no field here. (The engine-posted auto-close
// and reminder lines are system announcements, not broadcaster voice: they
// keep their localized defaults.)
type raffleConfig struct {
	OpenedMessage   string `json:"openedMessage"`   // i18n raffle.opened      {mins}
	JoinMessage     string `json:"joinMessage"`     // i18n raffle.joined      {user} {count}
	AlreadyMessage  string `json:"alreadyMessage"`  // i18n raffle.join.already {user} {count}
	NoRaffleMessage string `json:"noRaffleMessage"` // i18n raffle.join.closed {user}
	WonMessage      string `json:"wonMessage"`      // i18n raffle.won         {targets} {count} {entrants} {claim}
	ClaimOkMessage  string `json:"claimOkMessage"`  // i18n raffle.claim.ok    {user}
}

// raffleCmd bundles the per-invocation state every handler shares (the store,
// the message context, the decoded config, the logger) so each handler is a
// method taking only its own arguments instead of threading the same five
// values through every call.
type raffleCmd struct {
	r   engine.RaffleStore
	c   *module.Context
	cfg raffleConfig
	log *zap.Logger
}

// newRaffleCmd assembles the shared state for one invocation, decoding the
// broadcaster's reply-template overrides. ok is false when the raffle store is
// absent (the module is inert), so callers return without acting.
func newRaffleCmd(d engine.Deps, c *module.Context, log *zap.Logger) (rc raffleCmd, ok bool) {
	if d.Raffle == nil {
		return raffleCmd{}, false
	}
	rc = raffleCmd{r: d.Raffle, c: c, log: log}
	_ = c.Decode(&rc.cfg)
	return rc, true
}

// raffleStandalone adapts a raffleCmd method into a RunFunc for the
// argument-less commands (!join, !claim, !winner).
func raffleStandalone(d engine.Deps, log *zap.Logger, fn func(raffleCmd, context.Context, module.Emit) error) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		rc, ok := newRaffleCmd(d, c, log)
		if !ok {
			return nil
		}
		return fn(rc, ctx, emit)
	}
}

// raffleRoute is one !raffle subcommand's dispatch row: the handler plus
// whether it is moderator-gated. The gate lives in the router, so every route
// spends zero branches on permission itself.
type raffleRoute struct {
	mod bool
	run func(rc raffleCmd, ctx context.Context, args string, emit module.Emit) error
}

// raffleRoutes maps each !raffle subcommand ("" for bare !raffle) to its row.
// A subcommand not in the table gets usage; a mod-only row typed by a non-mod
// is silently ignored, matching the engine gate's silence.
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

// raffleDispatch handles !raffle and routes its subcommands. The engine's
// command gate runs it for everyone; per-row mod flags re-check the role.
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

// status answers a bare !raffle with whether one runs, its pool size and the
// minutes left on its deadline.
func (rc raffleCmd) status(ctx context.Context, emit module.Emit) error {
	st, err := rc.r.Status(ctx, rc.c.BroadcasterID)
	if err != nil {
		rc.log.Warn("raffle: status failed", rc.bid(), zap.Error(err))
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

// optInt parses one optional integer argument; ok=false leaves the field at
// the caller's default. Non-numeric text (a stray word after !raffle open) is
// simply not a number.
func optInt(s string) (n int64, ok bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// parseOpenArgs decodes "!raffle open [minutes] [winners] [remind]". Absent
// minutes/winners keep their zero values (module and store defaults); an
// absent remind keeps zero (store's default cadence), while an explicit
// remind of zero-or-less becomes a negative duration — the store's explicit
// disable.
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

// open starts a raffle from "<minutes> <winners> <remind>" args; all optional,
// all clamped by the store. remind is the reminder cadence in minutes — 0 or
// negative disables the time-left ticker, empty uses the store's default.
// ok=false means the deadline gate found one running.
func (rc raffleCmd) open(ctx context.Context, args string, emit module.Emit) error {
	minutes, winners, remind := parseOpenArgs(args)

	ok, err := rc.r.Open(ctx, rc.c.BroadcasterID, engine.RaffleOpenSpec{
		OpenedBy: strings.ToLower(rc.c.Env.ChatterUserLogin),
		Winners:  winners,
		Duration: time.Duration(minutes) * time.Minute,
		Remind:   remind,
	})
	if err != nil {
		rc.log.Warn("raffle: open failed", rc.bid(), zap.Error(err))
		return err
	}
	if ok {
		rc.reply(emit, rc.cfg.OpenedMessage, "raffle.opened", "mins", strconv.FormatInt(minutes, 10))
	} else {
		rc.reply(emit, "", "raffle.open.already")
	}
	return nil
}

// join enters the invoking chatter. Joining twice answers with the standing
// entry count rather than re-adding (the ZADD NX guarantee).
func (rc raffleCmd) join(ctx context.Context, emit module.Emit) error {
	login := strings.ToLower(rc.c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}
	entry, err := rc.r.Join(ctx, rc.c.BroadcasterID, login)
	if err != nil {
		rc.log.Warn("raffle: join failed", rc.bid(), zap.Error(err))
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

// draw closes the raffle now and announces. countOverride parses an explicit
// winner count; empty/invalid falls back to the state's configured count.
func (rc raffleCmd) draw(ctx context.Context, arg string, emit module.Emit) error {
	override := int64(0)
	if n, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64); err == nil && n > 0 {
		override = n
	}
	res, err := rc.r.Draw(ctx, rc.c.BroadcasterID, override)
	if err != nil {
		rc.log.Warn("raffle: draw failed", rc.bid(), zap.Error(err))
		return err
	}
	if res == nil {
		rc.reply(emit, "", "raffle.status.closed")
		return nil
	}
	rc.announceResult(emit, res)
	return nil
}

// cancel tears the running raffle down without drawing anything.
func (rc raffleCmd) cancel(ctx context.Context, emit module.Emit) error {
	ok, err := rc.r.Cancel(ctx, rc.c.BroadcasterID)
	if err != nil {
		rc.log.Warn("raffle: cancel failed", rc.bid(), zap.Error(err))
		return err
	}
	if ok {
		rc.reply(emit, "", "raffle.cancelled")
	} else {
		rc.reply(emit, "", "raffle.status.closed")
	}
	return nil
}

// last recalls the previous draw's winners for !winner, splitting confirmed
// claims from unclaimed prizes once any winner has used !claim.
func (rc raffleCmd) last(ctx context.Context, emit module.Emit) error {
	res, found, err := rc.r.LastResult(ctx, rc.c.BroadcasterID)
	if err != nil {
		rc.log.Warn("raffle: last failed", rc.bid(), zap.Error(err))
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

// claimConfirm handles a winner's !claim against the latest draw. Non-winners
// share the no-prize reply — chat has no business learning who did or didn't
// win beyond what the announcement already said.
func (rc raffleCmd) claimConfirm(ctx context.Context, emit module.Emit) error {
	login := strings.ToLower(rc.c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}
	outcome, err := rc.r.Claim(ctx, rc.c.BroadcasterID, login)
	if err != nil {
		rc.log.Warn("raffle: claim failed", rc.bid(), zap.Error(err))
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

// announceResult posts a draw outcome shared by the manual command and usable
// verbatim by tests: empty pool says nobody won, otherwise the winners line.
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

// mentionTargets renders winners as chat mentions: "@a, @b". Entries are
// stored as logins (the queue precedent), so a prefix is all it takes.
func mentionTargets(winners []string) string {
	prefixed := make([]string, len(winners))
	for i, w := range winners {
		prefixed[i] = "@" + w
	}
	return strings.Join(prefixed, ", ")
}

// reply emits one localized system line. kv are {token},value pairs; {user}
// (the invoking chatter) is always available.
// reply emits one chat line. override is the broadcaster's customized template
// for this reply ("" for the fixed system lines, or an uncustomized
// customizable one); when empty the localized default for key is used. kv are
// {token},value pairs (token names without braces); {user} (the invoking
// chatter) and the generic dynamic vars ({random}, {choice:…}) are always
// available, so a customized template can use them too.
func (rc raffleCmd) reply(emit module.Emit, override, key string, kv ...string) {
	tmpl := override
	if tmpl == "" {
		tmpl = i18n.T(rc.c.Locale, key)
	}
	text := module.ExpandString(tmpl, func(k string) (string, bool) {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == k {
				return kv[i+1], true
			}
		}
		if k == "user" {
			return rc.c.Env.ChatterUserLogin, true
		}
		return module.ParseDynamic(k)
	})
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: rc.c.Env.BroadcasterUserID,
		Text:          text,
	})
}

// bid is the broadcaster-id log field, shared by every handler's warn path.
func (rc raffleCmd) bid() zap.Field { return zap.Uint64("broadcaster_id", rc.c.BroadcasterID) }
