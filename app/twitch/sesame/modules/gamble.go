// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"math"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"

	"go.uber.org/zap"
)

const gambleModuleName = "gamble"

func gambleCooldownKey(broadcasterID uint64, login string) string {
	return "games:gamble:" + strconv.FormatUint(broadcasterID, 10) + ":" + login
}

func Gamble(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(gambleModuleName, module.KindOptIn)
	m.Command("gamble").Everyone().Run(gambleRun(d, log))
	return m.Build()
}

type gambleConfig struct {
	MinBet          int64  `json:"minBet"`
	MaxBet          int64  `json:"maxBet"`
	WinPercent      int64  `json:"winPercent"`
	CooldownSeconds int64  `json:"cooldownSeconds"`
	PointsName      string `json:"pointsName"`
	WinMessage      string `json:"winMessage"`
	LoseMessage     string `json:"loseMessage"`
}

type gambleCmd struct {
	chatReplier
	d       engine.Deps
	c       *module.Context
	cfg     engine.GambleSettings
	tmpl    gambleConfig
	balance int64
	log     *zap.Logger
}

func newGambleCmd(ctx context.Context, d engine.Deps, c *module.Context, log *zap.Logger) (gc gambleCmd, ok bool) {
	if d.Loyalty == nil {
		return gambleCmd{}, false
	}
	var raw gambleConfig
	_ = c.Decode(&raw)
	points, on := loyaltyVoice(ctx, d, c, raw.PointsName)
	if !on {
		return gambleCmd{}, false
	}
	gc = gambleCmd{
		chatReplier: newGameReplier(c, points),
		d:           d,
		c:           c,
		cfg:         engine.ClampGambleSettings(raw.MinBet, raw.MaxBet, raw.WinPercent, raw.CooldownSeconds),
		tmpl:        raw,
		log:         log,
	}
	return gc, true
}

func gambleRun(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		gc, ok := newGambleCmd(ctx, d, c, log)
		if !ok {
			return nil
		}
		return gc.run(ctx, args, emit)
	}
}

func (gc gambleCmd) run(ctx context.Context, arg string, emit module.Emit) error {
	login := strings.ToLower(gc.c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}

	bal, err := gc.d.Loyalty.BalanceGet(ctx, gc.c.BroadcasterID, gc.viewerID())
	if err != nil {
		gc.log.Warn("gamble: balance read failed", gc.c.BID(), zap.Error(err))
		return err
	}
	gc.balance = bal.Points

	bet, refused := gc.refuse(arg)
	if refused.key != "" {
		gc.reply(emit, "", refused.key, refused.tokens...)
		return nil
	}

	allowed, err := gc.claimCooldown(ctx, login)
	if err != nil {
		return err
	}
	if !allowed {
		gc.reply(emit, "", "gamble.cool", "secs", strconv.FormatInt(gc.cfg.CooldownSeconds, 10))
		return nil
	}

	return gc.settle(ctx, login, wagerOutcome{bet: bet}, emit)
}

func (gc gambleCmd) settle(ctx context.Context, login string, wager wagerOutcome, emit module.Emit) error {
	roll, err := engine.RollGamble()
	if err != nil {
		gc.log.Warn("gamble: roll failed", gc.c.BID(), zap.Error(err))
		return err
	}
	wager.roll = roll
	out, err := gc.d.Loyalty.BalanceWager(ctx, engine.PointWager{BroadcasterID: gc.c.BroadcasterID, ViewerID: gc.viewerID(), Login: login, Amount: wager.bet, Won: engine.GambleWins(roll, gc.cfg.WinPercent)})
	if err != nil {
		gc.log.Warn("gamble: settlement failed", gc.c.BID(), zap.Error(err))
		return err
	}
	if !gc.acceptSettlement(out, emit) {
		return nil
	}
	wager.balance = out.Balance.Points
	if engine.GambleWins(roll, gc.cfg.WinPercent) {
		gc.announce(emit, gc.tmpl.WinMessage, "gamble.win", wager)
		return nil
	}
	gc.reply(emit, gc.tmpl.LoseMessage, "gamble.lose",
		"roll", strconv.FormatInt(roll, 10),
		"chance", strconv.FormatInt(gc.cfg.WinPercent, 10),
		"amount", strconv.FormatInt(wager.bet, 10),
		"balance", strconv.FormatInt(wager.balance, 10))
	return nil
}

func (gc gambleCmd) acceptSettlement(out engine.WagerOutcome, emit module.Emit) bool {
	switch {
	case !out.Found:
		gc.reply(emit, "", "gamble.unknown")
	case out.LimitExceeded:
		gc.reply(emit, "", "gamble.range")
	case !out.Applied:
		gc.reply(emit, "", "gamble.broke", "balance", strconv.FormatInt(out.Balance.Points, 10))
	default:
		return true
	}
	return false
}

type wagerOutcome struct {
	roll    int64
	bet     int64
	balance int64
}

type refusal struct {
	key    replyKey
	tokens []string
}

func (gc gambleCmd) refuse(arg string) (int64, refusal) {
	bet, outcome := engine.ResolveGambleBet(arg, gc.balance, gc.cfg.MinBet, gc.cfg.MaxBet)
	switch outcome {
	case engine.BetOK:
		return gc.acceptBet(bet)
	case engine.BetEmpty, engine.BetInvalid:
		return 0, refusal{key: "gamble.usage"}
	case engine.BetBelowMin:
		return 0, refusal{key: "gamble.min", tokens: boundKV("min", gc.cfg.MinBet)}
	case engine.BetAboveMax:
		return 0, refusal{key: "gamble.max", tokens: boundKV("max", gc.cfg.MaxBet)}
	default:
		return 0, refusal{key: "gamble.broke", tokens: []string{"balance", strconv.FormatInt(gc.balance, 10)}}
	}
}

// Refuse a wager whose possible net win exceeds the cached balance capacity.
func (gc gambleCmd) acceptBet(bet int64) (int64, refusal) {
	if gc.balance > math.MaxInt64-bet {
		return 0, refusal{key: "gamble.range"}
	}
	return bet, refusal{}
}

func boundKV(name string, limit int64) []string {
	return []string{name, strconv.FormatInt(limit, 10)}
}

func (gc gambleCmd) claimCooldown(ctx context.Context, login string) (bool, error) {
	if gc.d.Cooldown == nil || gc.cfg.CooldownSeconds <= 0 {
		return true, nil
	}
	allowed, err := gc.d.Cooldown.Allow(ctx,
		gambleCooldownKey(gc.c.BroadcasterID, login),
		engine.GambleCooldown(gc.cfg.CooldownSeconds))
	if err != nil {
		gc.log.Warn("gamble: cooldown check failed", gc.c.BID(), zap.Error(err))
		return false, err
	}
	return allowed, nil
}

func (gc gambleCmd) announce(emit module.Emit, override string, key replyKey, wager wagerOutcome) {
	gc.reply(emit, override, key,
		"roll", strconv.FormatInt(wager.roll, 10),
		"chance", strconv.FormatInt(gc.cfg.WinPercent, 10),
		"amount", strconv.FormatInt(wager.bet, 10),
		"balance", strconv.FormatInt(wager.balance, 10))
}

func (gc gambleCmd) viewerID() uint64 {
	id, _ := strconv.ParseUint(gc.c.Env.ChatterUserID, 10, 64)
	return id
}
