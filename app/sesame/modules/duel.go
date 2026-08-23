// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"

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
	gameReplier
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
	dc = duelCmd{
		gameReplier: newGameReplier(c, raw.PointsName),
		s:           d.Duel,
		c:           c,
		cfg: duelClamps{
			MinStake: minOr(raw.MinStake, duelMinStakeFloor),
			// The store refuses escrows above its ceiling, so a configured
			// max beyond it would only turn every wager into an error.
			MaxStake: min(maxOr(raw.MaxStake, raw.MinStake, duelDefaultMaxStake), engine.DuelMaxStake),
		},
		t:   raw,
		log: log,
	}
	return dc, true
}

// minOr falls back to def when unset; maxOr raises the ceiling to at least
// the configured floor, so an inverted config can never refuse every stake.
func minOr(v, def int64) int64 {
	if v > 0 {
		return v
	}
	return def
}

func maxOr(maxV, floor, def int64) int64 {
	if maxV <= 0 {
		maxV = def
	}
	return max(maxV, floor)
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

	handled, err := dc.routeKeyword(ctx, login, first, emit)
	switch {
	case handled || err != nil:
		return err
	case isChallengeShape(first):
		return dc.challenge(ctx, login, splitChallenge(first, rest), emit)
	default:
		return dc.stake(ctx, login, first, emit)
	}
}

// routeKeyword answers the four spelled-out subcommands; handled reports
// whether first was one of them at all.
func (dc duelCmd) routeKeyword(ctx context.Context, login, first string, emit module.Emit) (handled bool, err error) {
	switch strings.ToLower(first) {
	case "":
		return true, dc.status(ctx, emit)
	case "accept":
		return true, dc.accept(ctx, login, emit)
	case "decline":
		return true, dc.decline(ctx, login, emit)
	case "cancel":
		return true, dc.cancel(ctx, login, emit)
	}
	return false, nil
}

// isChallengeShape reports whether the head token can open a challenge at
// all: anything non-numeric (a bare number rode through means "!duel <stake>").
func isChallengeShape(first string) bool {
	_, err := strconv.ParseInt(strings.TrimPrefix(first, "@"), 10, 64)
	return err != nil && first != ""
}

// challengeReq is one parsed "!duel <user> <stake>" invocation.
type challengeReq struct {
	target string
	stake  string
}

// splitChallenge splits "<user> <stake>"; callers checked isChallengeShape,
// so the stake must parse or the whole request degrades to usage.
func splitChallenge(first, rest string) challengeReq {
	stakeArg, _ := splitFirst(rest)
	return challengeReq{
		target: strings.TrimPrefix(strings.ToLower(first), "@"),
		stake:  stakeArg,
	}
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
	tokens := []token{
		tk("opener", st.Opener),
		tk("target", st.Challenged),
		tk("stake", strconv.FormatInt(st.Stake, 10)),
		tk("pot", strconv.FormatInt(st.Pot, 10)),
		tk("count", strconv.FormatInt(st.Entrants, 10)),
		tk("secs", strconv.FormatInt(st.SecondsLeft, 10)),
	}
	key := replyKey("duel.status.pot")
	if st.Kind == engine.DuelChallenge {
		key = "duel.status.challenge"
	}
	dc.reply(emit, "", key, tokens...)
	return nil
}

// stake handles "!duel <n>": opening a pot duel when none runs, joining the
// pot that does, and reporting a pending challenge that blocks both.
func (dc duelCmd) stake(ctx context.Context, login, raw string, emit module.Emit) error {
	stake, refused := dc.resolveStake(raw)
	if refused != "" {
		dc.refuseReply(emit, refused)
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
	case !res.Open:
		// Nothing was running when we asked: open instead. A race loser here
		// just re-runs into the open path's own busy report.
		return dc.openPot(ctx, login, stake, emit)
	default:
		return dc.replyJoin(res, stake, emit)
	}
	return nil
}

// replyJoin answers a resolved join attempt against the running pot: in,
// already seated, or refused on funds/identity.
func (dc duelCmd) replyJoin(res engine.DuelJoinResult, stake int64, emit module.Emit) error {
	switch {
	case res.Joined:
		dc.reply(emit, dc.t.JoinMessage, "duel.joined",
			tk("stake", strconv.FormatInt(stake, 10)),
			tk("count", strconv.FormatInt(res.Entrants, 10)),
			tk("pot", strconv.FormatInt(res.Pot, 10)))
	case res.Already:
		dc.reply(emit, "", "duel.join.already",
			tk("count", strconv.FormatInt(res.Entrants, 10)),
			tk("pot", strconv.FormatInt(res.Pot, 10)))
	case res.Unknown:
		dc.reply(emit, "", "duel.join.unknown")
	case res.Short:
		dc.reply(emit, "", "duel.join.short")
	default:
		dc.reply(emit, "", "duel.err")
	}
	return nil
}

// replyOpen maps the three outcomes every Open caller shares — started, slot
// busy, or the opener could not escrow — leaving only the success line to
// each caller.
func (dc duelCmd) replyOpen(emit module.Emit, res engine.DuelOpenResult, started func()) bool {
	switch {
	case res.Started:
		started()
	case res.Busy:
		dc.reply(emit, "", "duel.open.busy")
	case res.Short:
		dc.reply(emit, "", "duel.join.short")
	case res.Unknown:
		dc.reply(emit, "", "duel.join.unknown")
	default:
		dc.reply(emit, "", "duel.err")
	}
	return res.Started
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
	dc.replyOpen(emit, res, func() {
		dc.reply(emit, dc.t.OpenedMessage, "duel.opened",
			tk("secs", strconv.FormatInt(engine.ClampDuelSeconds(dc.t.PotSeconds, engine.DuelDefaultPotSeconds), 10)),
			tk("stake", strconv.FormatInt(stake, 10)))
	})
	return nil
}

// challenge starts a direct duel: "!duel <user> <stake>".
func (dc duelCmd) challenge(ctx context.Context, login string, req challengeReq, emit module.Emit) error {
	if req.target == "" {
		dc.reply(emit, "", "duel.usage")
		return nil
	}
	if req.target == login {
		dc.reply(emit, "", "duel.challenge.self")
		return nil
	}
	stake, refused := dc.resolveStake(req.stake)
	if refused != "" {
		dc.refuseReply(emit, refused)
		return nil
	}
	res, err := dc.s.Open(ctx, dc.c.BroadcasterID, engine.DuelOpenSpec{
		Kind:             engine.DuelChallenge,
		Opener:           login,
		Challenged:       req.target,
		Stake:            stake,
		ChallengeSeconds: dc.t.ChallengeSeconds,
	})
	if err != nil {
		dc.log.Warn("duel: challenge failed", dc.bid(), zap.Error(err))
		return err
	}
	dc.replyOpen(emit, res, func() {
		dc.reply(emit, dc.t.ChallengeMessage, "duel.challenge.sent",
			tk("target", req.target),
			tk("stake", strconv.FormatInt(stake, 10)),
			tk("pot", strconv.FormatInt(stake*2, 10)),
			tk("secs", strconv.FormatInt(engine.ClampDuelSeconds(dc.t.ChallengeSeconds, engine.DuelDefaultChallengeSeconds), 10)))
	})
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
		dc.replySettled(res, emit)
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

// replySettled announces an accepted duel: the clean win line, or the
// pending-payout line when the credit failed after settlement.
func (dc duelCmd) replySettled(res engine.DuelAcceptResult, emit module.Emit) {
	if res.Unpaid {
		// Settled on paper, credit failed: name the winner and pot so chat
		// knows the outcome while the payout lands.
		dc.reply(emit, "", "duel.payout_pending",
			tk("winner", res.Winner),
			tk("pot", strconv.FormatInt(res.Pot, 10)))
		return
	}
	dc.reply(emit, dc.t.WonMessage, "duel.won",
		tk("winner", res.Winner),
		tk("loser", res.Loser),
		tk("pot", strconv.FormatInt(res.Pot, 10)))
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
			tk("opener", res.Opener),
			tk("refund", strconv.FormatInt(res.Refund, 10)))
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
			tk("refunds", strconv.FormatInt(res.Refunded, 10)),
			tk("total", strconv.FormatInt(res.Total, 10)))
	case res.Found && !res.Allowed:
		dc.reply(emit, "", "duel.cancel.denied")
	case res.Busy:
		dc.reply(emit, "", "duel.busy")
	default:
		dc.reply(emit, "", "duel.status.none")
	}
	return nil
}

// stakeRefusal names why a stake was refused; empty means it stands.
type stakeRefusal replyKey

const (
	refUsage    stakeRefusal = "duel.usage"
	refMinStake stakeRefusal = "duel.stake.min"
	refMaxStake stakeRefusal = "duel.stake.max"
)

// resolveStake parses and bounds one stake argument against the channel's
// limits.
func (dc duelCmd) resolveStake(raw string) (int64, stakeRefusal) {
	n, err := strconv.ParseInt(strings.TrimPrefix(raw, "@"), 10, 64)
	if err != nil || n <= 0 {
		return 0, refUsage
	}
	switch {
	case n < dc.cfg.MinStake:
		return 0, refMinStake
	case n > dc.cfg.MaxStake:
		return 0, refMaxStake
	}
	return n, ""
}

// reply emits a stake refusal, carrying the bound it tripped.
func (dc duelCmd) refuseReply(emit module.Emit, refused stakeRefusal) {
	key := replyKey(refused)
	switch refused {
	case refMinStake:
		dc.reply(emit, "", key, tk("min", strconv.FormatInt(dc.cfg.MinStake, 10)))
	case refMaxStake:
		dc.reply(emit, "", key, tk("max", strconv.FormatInt(dc.cfg.MaxStake, 10)))
	default:
		dc.reply(emit, "", key)
	}
}

func (dc duelCmd) bid() zap.Field { return zap.Uint64("broadcaster_id", dc.c.BroadcasterID) }
