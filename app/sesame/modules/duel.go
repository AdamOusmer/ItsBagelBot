// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// duelModuleName is the ModuleView key; the console MODULE_CATALOG entry and
// the dashboard module page use the same id.
const duelModuleName = "duel"

// duelDefaultMaxStake mirrors the gamble module's max-bet default so both
// games feel like one economy out of the box; the store's DuelMaxStake is the
// sanity ceiling behind whatever a broadcaster configures here.
const duelDefaultMaxStake = int64(1000)

// duelMinStakeFloor is the lowest stake any channel may configure.
const duelMinStakeFloor = int64(1)

// Duel owns the channel's single duel slot in both flavors:
//
//	!duel                    → status
//	!duel <points>           → open a pot duel, or join one that is running
//	!duel <user> <points>    → challenge someone; they accept with !duel accept
//	!duel accept|decline     → answer a pending challenge (challenged party only)
//	!duel cancel             → opener or mod tears it down with full refunds
//
// Pot duels draw a winner weighted by stake when their window closes;
// challenges resolve on acceptance — equal stakes, fair coin, winner takes
// both. Every point movement escrows through the loyalty service's
// conditional debit, and every ending (draw, no-show, decline, cancel) pays
// or refunds from the ledger, so nothing rides on trust.
func Duel(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(duelModuleName, module.KindOptIn)
	m.Command("duel").Everyone().Run(duelRun(d, log))
	return m.Build()
}

// duelConfig holds the broadcaster-tunable knobs and the customizable
// conversational replies; empty templates fall back to their localized
// defaults.
type duelConfig struct {
	MinStake         int64  `json:"minStake"`         // default 1
	MaxStake         int64  `json:"maxStake"`         // default 1000
	PotSeconds       int64  `json:"potSeconds"`       // default 60
	ChallengeSeconds int64  `json:"challengeSeconds"` // default 120
	PointsName       string `json:"pointsName"`       // currency word in money lines
	OpenedMessage    string `json:"openedMessage"`    // i18n duel.opened
	JoinMessage      string `json:"joinMessage"`      // i18n duel.joined
	WonMessage       string `json:"wonMessage"`       // i18n duel.won
	ChallengeMessage string `json:"challengeMessage"` // i18n duel.challenge.sent
}

// duelCmd bundles the per-invocation state every handler shares.
type duelCmd struct {
	s   engine.DuelStore
	c   *module.Context
	cfg duelClamps
	t   duelConfig
	log *zap.Logger
}

// duelClamps is the clamped view of the numeric config.
type duelClamps struct {
	MinStake, MaxStake int64
}

// newDuelCmd assembles the shared state for one invocation. ok=false means
// the duel store is absent (module inert).
func newDuelCmd(d engine.Deps, c *module.Context, log *zap.Logger) (dc duelCmd, ok bool) {
	if d.Duel == nil {
		return duelCmd{}, false
	}
	var raw duelConfig
	_ = c.Decode(&raw)
	maxStake := raw.MaxStake
	if maxStake <= 0 {
		maxStake = duelDefaultMaxStake
	}
	minStake := raw.MinStake
	if minStake <= 0 {
		minStake = duelMinStakeFloor
	}
	dc = duelCmd{
		s:   d.Duel,
		c:   c,
		cfg: duelClamps{MinStake: minStake, MaxStake: max(maxStake, minStake)},
		t:   raw,
		log: log,
	}
	if strings.TrimSpace(dc.t.PointsName) == "" {
		dc.t.PointsName = "points"
	}
	return dc, true
}

func duelRun(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		dc, ok := newDuelCmd(d, c, log)
		if !ok {
			return nil
		}
		return dc.route(ctx, args, emit)
	}
}

// route dispatches "!duel" by argument shape rather than subcommand table:
// keywords first, then "<user> <stake>" (a challenge), then "<stake>" (open
// or join). Anything else gets usage.
func (dc duelCmd) route(ctx context.Context, args string, emit module.Emit) error {
	login := strings.ToLower(dc.c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}
	first, rest := splitFirst(args)
	switch strings.ToLower(first) {
	case "":
		return dc.status(ctx, emit)
	case "accept":
		return dc.accept(ctx, login, emit)
	case "decline":
		return dc.decline(ctx, login, emit)
	case "cancel":
		return dc.cancel(ctx, login, emit)
	}
	if target, stakeArg, isChallenge := splitChallenge(first, rest); isChallenge {
		return dc.challenge(ctx, login, target, stakeArg, emit)
	}
	return dc.stake(ctx, login, first, emit)
}

// splitChallenge reports whether "<first> <rest>" names a challenge: a
// non-numeric target followed by a stake number.
func splitChallenge(first, rest string) (target, stakeArg string, ok bool) {
	if _, err := strconv.ParseInt(strings.TrimPrefix(first, "@"), 10, 64); err == nil {
		return "", "", false // a bare number rode through: not a challenge
	}
	stake, _ := splitFirst(rest)
	if stake == "" {
		return "", "", false // "!duel bob" alone is not enough to challenge
	}
	if _, err := strconv.ParseInt(stake, 10, 64); err != nil {
		return "", "", false
	}
	return strings.TrimPrefix(strings.ToLower(first), "@"), stake, true
}

// status answers a bare !duel with what, if anything, runs.
func (dc duelCmd) status(ctx context.Context, emit module.Emit) error {
	st, err := dc.s.Status(ctx, dc.c.BroadcasterID)
	if err != nil {
		dc.log.Warn("duel: status failed", dc.bid(), zap.Error(err))
		return err
	}
	if !st.Open {
		dc.reply(emit, "", "duel.status.none")
		return nil
	}
	kv := []string{
		"opener", st.Opener,
		"target", st.Challenged,
		"stake", strconv.FormatInt(st.Stake, 10),
		"pot", strconv.FormatInt(st.Pot, 10),
		"count", strconv.FormatInt(st.Entrants, 10),
		"secs", strconv.FormatInt(st.SecondsLeft, 10),
	}
	if st.Kind == engine.DuelChallenge {
		dc.reply(emit, "", "duel.status.challenge", kv...)
		return nil
	}
	dc.reply(emit, "", "duel.status.pot", kv...)
	return nil
}

// stake handles "!duel <n>": opening a pot duel when none runs, joining the
// pot that does, and reporting a pending challenge that blocks both.
func (dc duelCmd) stake(ctx context.Context, login, raw string, emit module.Emit) error {
	stake, outcome := dc.resolveStake(raw)
	if outcome != "" {
		dc.replyStakeRefusal(emit, outcome)
		return nil
	}

	res, err := dc.s.Join(ctx, dc.c.BroadcasterID, login, stake)
	if err != nil {
		dc.log.Warn("duel: join failed", dc.bid(), zap.Error(err))
		return err
	}
	switch {
	case res.ChallengePending:
		dc.reply(emit, "", "duel.challenge.pending")
	case res.Busy:
		dc.reply(emit, "", "duel.busy")
	case res.Joined:
		dc.reply(emit, dc.t.JoinMessage, "duel.joined",
			"stake", strconv.FormatInt(stake, 10),
			"count", strconv.FormatInt(res.Entrants, 10),
			"pot", strconv.FormatInt(res.Pot, 10))
	case res.Already:
		dc.reply(emit, "", "duel.join.already",
			"count", strconv.FormatInt(res.Entrants, 10),
			"pot", strconv.FormatInt(res.Pot, 10))
	case res.Unknown:
		dc.reply(emit, "", "duel.join.unknown")
	case res.Short:
		dc.reply(emit, "", "duel.join.short")
	case !res.Open:
		// Nothing was running when we asked: open instead. A race loser here
		// just re-runs into the open path's own busy report.
		return dc.openPot(ctx, login, stake, emit)
	default:
		dc.reply(emit, "", "duel.err")
	}
	return nil
}

// openPot starts a pot duel from an opener stake.
func (dc duelCmd) openPot(ctx context.Context, login string, stake int64, emit module.Emit) error {
	res, err := dc.s.Open(ctx, dc.c.BroadcasterID, engine.DuelOpenSpec{
		Kind:       engine.DuelPot,
		Opener:     login,
		Stake:      stake,
		PotSeconds: dc.t.PotSeconds,
	})
	if err != nil {
		dc.log.Warn("duel: open failed", dc.bid(), zap.Error(err))
		return err
	}
	switch {
	case res.Started:
		dc.reply(emit, dc.t.OpenedMessage, "duel.opened",
			"secs", strconv.FormatInt(engine.ClampDuelSeconds(dc.t.PotSeconds, engine.DuelDefaultPotSeconds), 10),
			"stake", strconv.FormatInt(stake, 10))
	case res.Busy:
		dc.reply(emit, "", "duel.open.busy")
	case res.Short:
		dc.reply(emit, "", "duel.join.short")
	case res.Unknown:
		dc.reply(emit, "", "duel.join.unknown")
	default:
		dc.reply(emit, "", "duel.err")
	}
	return nil
}

// challenge starts a direct duel: "!duel <user> <stake>".
func (dc duelCmd) challenge(ctx context.Context, login, target, stakeArg string, emit module.Emit) error {
	if target == login {
		dc.reply(emit, "", "duel.challenge.self")
		return nil
	}
	stake, outcome := dc.resolveStake(stakeArg)
	if outcome != "" {
		dc.replyStakeRefusal(emit, outcome)
		return nil
	}
	res, err := dc.s.Open(ctx, dc.c.BroadcasterID, engine.DuelOpenSpec{
		Kind:             engine.DuelChallenge,
		Opener:           login,
		Challenged:       target,
		Stake:            stake,
		ChallengeSeconds: dc.t.ChallengeSeconds,
	})
	if err != nil {
		dc.log.Warn("duel: challenge failed", dc.bid(), zap.Error(err))
		return err
	}
	switch {
	case res.Started:
		dc.reply(emit, dc.t.ChallengeMessage, "duel.challenge.sent",
			"target", target,
			"stake", strconv.FormatInt(stake, 10),
			"pot", strconv.FormatInt(stake*2, 10),
			"secs", strconv.FormatInt(engine.ClampDuelSeconds(dc.t.ChallengeSeconds, engine.DuelDefaultChallengeSeconds), 10))
	case res.Busy:
		dc.reply(emit, "", "duel.open.busy")
	case res.Short:
		dc.reply(emit, "", "duel.join.short")
	case res.Unknown:
		dc.reply(emit, "", "duel.join.unknown")
	default:
		dc.reply(emit, "", "duel.err")
	}
	return nil
}

// accept resolves a pending challenge by the challenged party's own hand.
func (dc duelCmd) accept(ctx context.Context, login string, emit module.Emit) error {
	res, err := dc.s.Accept(ctx, dc.c.BroadcasterID, login)
	if err != nil {
		dc.log.Warn("duel: accept failed", dc.bid(), zap.Error(err))
		return err
	}
	switch {
	case res.Accepted:
		dc.reply(emit, dc.t.WonMessage, "duel.won",
			"winner", res.Winner,
			"loser", res.Loser,
			"pot", strconv.FormatInt(res.Pot, 10))
	case res.Found && res.WrongUser:
		dc.reply(emit, "", "duel.accept.notYou")
	case res.Short:
		dc.reply(emit, "", "duel.accept.short")
	case res.Unknown:
		dc.reply(emit, "", "duel.accept.unknown")
	case res.Busy:
		dc.reply(emit, "", "duel.busy")
	default:
		dc.reply(emit, "", "duel.accept.none")
	}
	return nil
}

// decline refuses a pending challenge; the opener's stake goes back.
func (dc duelCmd) decline(ctx context.Context, login string, emit module.Emit) error {
	res, err := dc.s.Decline(ctx, dc.c.BroadcasterID, login)
	if err != nil {
		dc.log.Warn("duel: decline failed", dc.bid(), zap.Error(err))
		return err
	}
	switch {
	case res.Declined:
		dc.reply(emit, "", "duel.decline.ok",
			"opener", res.Opener,
			"refund", strconv.FormatInt(res.Refund, 10))
	case res.Found && res.WrongUser:
		dc.reply(emit, "", "duel.accept.notYou")
	case res.Busy:
		dc.reply(emit, "", "duel.busy")
	default:
		dc.reply(emit, "", "duel.accept.none")
	}
	return nil
}

// cancel tears the running duel down with full refunds; the opener may always
// cancel their own duel, moderators any.
func (dc duelCmd) cancel(ctx context.Context, login string, emit module.Emit) error {
	mod := dc.c.Chatter().Allows(module.RoleModerator)
	res, err := dc.s.Cancel(ctx, dc.c.BroadcasterID, login, mod)
	if err != nil {
		dc.log.Warn("duel: cancel failed", dc.bid(), zap.Error(err))
		return err
	}
	switch {
	case res.Cancelled:
		dc.reply(emit, "", "duel.cancel.ok",
			"refunds", strconv.FormatInt(res.Refunded, 10),
			"total", strconv.FormatInt(res.Total, 10))
	case res.Found && !res.Allowed:
		dc.reply(emit, "", "duel.cancel.denied")
	case res.Busy:
		dc.reply(emit, "", "duel.busy")
	default:
		dc.reply(emit, "", "duel.status.none")
	}
	return nil
}

// resolveStake parses and bounds one stake argument, returning the i18n key
// of the refusal ("" when the stake stands).
func (dc duelCmd) resolveStake(raw string) (int64, string) {
	n, err := strconv.ParseInt(strings.TrimPrefix(raw, "@"), 10, 64)
	if err != nil || n <= 0 {
		return 0, "duel.usage"
	}
	switch {
	case n < dc.cfg.MinStake:
		return 0, "duel.stake.min"
	case n > dc.cfg.MaxStake:
		return 0, "duel.stake.max"
	}
	return n, ""
}

// replyStakeRefusal emits a stake refusal, carrying the bound it tripped.
func (dc duelCmd) replyStakeRefusal(emit module.Emit, key string) {
	switch key {
	case "duel.stake.min":
		dc.reply(emit, "", key, "min", strconv.FormatInt(dc.cfg.MinStake, 10))
	case "duel.stake.max":
		dc.reply(emit, "", key, "max", strconv.FormatInt(dc.cfg.MaxStake, 10))
	default:
		dc.reply(emit, "", key)
	}
}

// reply emits one localized line; kv are {token},value pairs, with {user} and
// the currency word always available. override is the broadcaster's
// customized template ("" for fixed system lines).
func (dc duelCmd) reply(emit module.Emit, override, key string, kv ...string) {
	tmpl := override
	if tmpl == "" {
		tmpl = i18n.T(dc.c.Locale, key)
	}
	text := module.ExpandString(tmpl, func(k string) (string, bool) {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == k {
				return kv[i+1], true
			}
		}
		switch k {
		case "user":
			return dc.c.Env.ChatterUserLogin, true
		case "points":
			return dc.t.PointsName, true
		}
		return module.ParseDynamic(k)
	})
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: dc.c.Env.BroadcasterUserID,
		Text:          text,
	})
}

func (dc duelCmd) bid() zap.Field { return zap.Uint64("broadcaster_id", dc.c.BroadcasterID) }
