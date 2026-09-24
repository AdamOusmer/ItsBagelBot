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

	// Escrow before the dice: the conditional debit is the wager, so overlapping bets cannot overpay.
	balance, ok, err := gc.escrow(ctx, login, bet, emit)
	if err != nil || !ok {
		return err
	}
	return gc.settle(ctx, login, wagerOutcome{bet: bet, balance: balance}, emit)
}

func (gc gambleCmd) settle(ctx context.Context, login string, wager wagerOutcome, emit module.Emit) error {
	roll, err := engine.RollGamble()
	if err != nil {
		gc.log.Warn("gamble: roll failed", gc.c.BID(), zap.Error(err))
		return err
	}
	wager.roll = roll
	if engine.GambleWins(roll, gc.cfg.WinPercent) {
		return gc.settleWin(ctx, login, wager, emit)
	}
	gc.reply(emit, gc.tmpl.LoseMessage, "gamble.lose",
		"roll", strconv.FormatInt(roll, 10),
		"chance", strconv.FormatInt(gc.cfg.WinPercent, 10),
		"amount", strconv.FormatInt(wager.bet, 10),
		"balance", strconv.FormatInt(wager.balance, 10))
	return nil
}

func (gc gambleCmd) escrow(ctx context.Context, login string, bet int64, emit module.Emit) (balance int64, ok bool, err error) {
	newBal, found, spent, err := gc.d.Loyalty.BalanceSpend(ctx, gc.c.BroadcasterID, login, bet)
	switch {
	case err != nil:
		gc.log.Warn("gamble: stake debit failed", gc.c.BID(), zap.Error(err))
		return 0, false, err
	case !found:
		gc.reply(emit, "", "gamble.unknown")
		return 0, false, nil
	case !spent:
		gc.reply(emit, "", "gamble.broke",
			"balance", strconv.FormatInt(newBal.Points, 10))
		return 0, false, nil
	}
	return newBal.Points, true, nil
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
		return bet, refusal{}
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

func (gc gambleCmd) settleWin(ctx context.Context, login string, wager wagerOutcome, emit module.Emit) error {
	newBal, found, err := gc.d.Loyalty.BalanceAdjust(ctx, gc.c.BroadcasterID, login, wager.bet*2, false)
	if err != nil {
		gc.log.Warn("gamble: win credit failed", gc.c.BID(), zap.Error(err))
		return err
	}
	if !found {
		gc.reply(emit, "", "gamble.err")
		return nil
	}
	wager.balance = newBal.Points
	gc.announce(emit, gc.tmpl.WinMessage, "gamble.win", wager)
	return nil
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
