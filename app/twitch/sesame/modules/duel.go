// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"

	"go.uber.org/zap"
)

const duelModuleName = "duel"

const duelDefaultMaxStake = int64(1000)

const duelMinStakeFloor = int64(1)

func Duel(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(duelModuleName, module.KindOptIn)
	m.Command("duel").Everyone().Run(duelRun(d, log))
	return m.Build()
}

type duelConfig struct {
	MinStake         int64  `json:"minStake"`
	MaxStake         int64  `json:"maxStake"`
	PotSeconds       int64  `json:"potSeconds"`
	ChallengeSeconds int64  `json:"challengeSeconds"`
	PointsName       string `json:"pointsName"`
	OpenedMessage    string `json:"openedMessage"`
	JoinMessage      string `json:"joinMessage"`
	WonMessage       string `json:"wonMessage"`
	ChallengeMessage string `json:"challengeMessage"`
}

type duelCmd struct {
	chatReplier
	s   engine.DuelStore
	c   *module.Context
	cfg duelClamps
	t   duelConfig
	log *zap.Logger
}

type duelClamps struct {
	MinStake, MaxStake int64
}

func newDuelCmd(ctx context.Context, d engine.Deps, c *module.Context, log *zap.Logger) (dc duelCmd, ok bool) {
	if d.Duel == nil {
		return duelCmd{}, false
	}
	var raw duelConfig
	_ = c.Decode(&raw)
	points, on := loyaltyVoice(ctx, d, c, raw.PointsName)
	if !on {
		return duelCmd{}, false
	}
	dc = duelCmd{
		chatReplier: newGameReplier(c, points),
		s:           d.Duel,
		c:           c,
		cfg: duelClamps{
			MinStake: minOr(raw.MinStake, duelMinStakeFloor),
			MaxStake: min(maxOr(raw.MaxStake, raw.MinStake, duelDefaultMaxStake), engine.DuelMaxStake),
		},
		t:   raw,
		log: log,
	}
	return dc, true
}

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
		dc, ok := newDuelCmd(ctx, d, c, log)
		if !ok {
			return nil
		}
		return dc.route(ctx, args, emit)
	}
}

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

func isChallengeShape(first string) bool {
	_, err := strconv.ParseInt(strings.TrimPrefix(first, "@"), 10, 64)
	return err != nil && first != ""
}

type challengeReq struct {
	target string
	stake  string
}

func splitChallenge(first, rest string) challengeReq {
	stakeArg, _ := splitFirst(rest)
	return challengeReq{
		target: strings.TrimPrefix(strings.ToLower(first), "@"),
		stake:  stakeArg,
	}
}

func (dc duelCmd) status(ctx context.Context, emit module.Emit) error {
	st, err := dc.s.Status(ctx, dc.c.BroadcasterID)
	if err != nil {
		dc.log.Warn("duel: status failed", dc.c.BID(), zap.Error(err))
		return err
	}
	if !st.Open {
		dc.reply(emit, "", "duel.status.none")
		return nil
	}
	tokens := []string{
		"opener", st.Opener,
		"target", st.Challenged,
		"stake", strconv.FormatInt(st.Stake, 10),
		"pot", strconv.FormatInt(st.Pot, 10),
		"count", strconv.FormatInt(st.Entrants, 10),
		"secs", strconv.FormatInt(st.SecondsLeft, 10),
	}
	key := replyKey("duel.status.pot")
	if st.Kind == engine.DuelChallenge {
		key = "duel.status.challenge"
	}
	dc.reply(emit, "", key, tokens...)
	return nil
}

func (dc duelCmd) stake(ctx context.Context, login, raw string, emit module.Emit) error {
	stake, refused := dc.resolveStake(raw)
	if refused != "" {
		dc.refuseReply(emit, refused)
		return nil
	}

	res, err := dc.s.Join(ctx, dc.c.BroadcasterID, login, stake)
	if err != nil {
		dc.log.Warn("duel: join failed", dc.c.BID(), zap.Error(err))
		return err
	}
	switch {
	case res.ChallengePending:
		dc.reply(emit, "", "duel.challenge.pending")
	case res.Busy:
		dc.reply(emit, "", "duel.busy")
	case !res.Open:
		return dc.openPot(ctx, login, stake, emit)
	default:
		return dc.replyJoin(res, stake, emit)
	}
	return nil
}

func (dc duelCmd) replyJoin(res engine.DuelJoinResult, stake int64, emit module.Emit) error {
	switch {
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
	default:
		dc.reply(emit, "", "duel.err")
	}
	return nil
}

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

func (dc duelCmd) openPot(ctx context.Context, login string, stake int64, emit module.Emit) error {
	res, err := dc.s.Open(ctx, dc.c.BroadcasterID, engine.DuelOpenSpec{
		Kind:       engine.DuelPot,
		Opener:     login,
		Stake:      stake,
		PotSeconds: dc.t.PotSeconds,
	})
	if err != nil {
		dc.log.Warn("duel: open failed", dc.c.BID(), zap.Error(err))
		return err
	}
	dc.replyOpen(emit, res, func() {
		dc.reply(emit, dc.t.OpenedMessage, "duel.opened",
			"secs", strconv.FormatInt(engine.ClampDuelSeconds(dc.t.PotSeconds, engine.DuelDefaultPotSeconds), 10),
			"stake", strconv.FormatInt(stake, 10))
	})
	return nil
}

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
		dc.log.Warn("duel: challenge failed", dc.c.BID(), zap.Error(err))
		return err
	}
	dc.replyOpen(emit, res, func() {
		dc.reply(emit, dc.t.ChallengeMessage, "duel.challenge.sent",
			"target", req.target,
			"stake", strconv.FormatInt(stake, 10),
			"pot", strconv.FormatInt(stake*2, 10),
			"secs", strconv.FormatInt(engine.ClampDuelSeconds(dc.t.ChallengeSeconds, engine.DuelDefaultChallengeSeconds), 10))
	})
	return nil
}

func (dc duelCmd) accept(ctx context.Context, login string, emit module.Emit) error {
	res, err := dc.s.Accept(ctx, dc.c.BroadcasterID, login)
	if err != nil {
		dc.log.Warn("duel: accept failed", dc.c.BID(), zap.Error(err))
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

func (dc duelCmd) replySettled(res engine.DuelAcceptResult, emit module.Emit) {
	if res.Unpaid {
		dc.reply(emit, "", "duel.payout_pending",
			"winner", res.Winner,
			"pot", strconv.FormatInt(res.Pot, 10))
		return
	}
	dc.reply(emit, dc.t.WonMessage, "duel.won",
		"winner", res.Winner,
		"loser", res.Loser,
		"pot", strconv.FormatInt(res.Pot, 10))
}

func (dc duelCmd) decline(ctx context.Context, login string, emit module.Emit) error {
	res, err := dc.s.Decline(ctx, dc.c.BroadcasterID, login)
	if err != nil {
		dc.log.Warn("duel: decline failed", dc.c.BID(), zap.Error(err))
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

func (dc duelCmd) cancel(ctx context.Context, login string, emit module.Emit) error {
	mod := dc.c.Chatter().Allows(module.RoleModerator)
	res, err := dc.s.Cancel(ctx, dc.c.BroadcasterID, login, mod)
	if err != nil {
		dc.log.Warn("duel: cancel failed", dc.c.BID(), zap.Error(err))
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

type stakeRefusal replyKey

const (
	refUsage    stakeRefusal = "duel.usage"
	refMinStake stakeRefusal = "duel.stake.min"
	refMaxStake stakeRefusal = "duel.stake.max"
)

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

func (dc duelCmd) refuseReply(emit module.Emit, refused stakeRefusal) {
	key := replyKey(refused)
	switch refused {
	case refMinStake:
		dc.reply(emit, "", key, "min", strconv.FormatInt(dc.cfg.MinStake, 10))
	case refMaxStake:
		dc.reply(emit, "", key, "max", strconv.FormatInt(dc.cfg.MaxStake, 10))
	default:
		dc.reply(emit, "", key)
	}
}
