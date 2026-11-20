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
	catalog "ItsBagelBot/internal/domain/analytics"

	"go.uber.org/zap"
)

const gambleModuleName = "gamble"

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
	MinBet          settingInt `json:"minBet"`
	MaxBet          settingInt `json:"maxBet"`
	WinPercent      settingInt `json:"winPercent"`
	CooldownSeconds settingInt `json:"cooldownSeconds"`
	PointsName      string     `json:"pointsName"`
	WinMessage      string     `json:"winMessage"`
	LoseMessage     string     `json:"loseMessage"`
}

type gambleCmd struct {
	chatReplier
	d        engine.Deps
	c        *module.Context
	cfg      engine.GambleSettings
	tmpl     gambleConfig
	balance  int64
	viewerID uint64
	log      *zap.Logger
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
		cfg:         engine.ClampGambleSettings(int64(raw.MinBet), int64(raw.MaxBet), int64(raw.WinPercent), int64(raw.CooldownSeconds)),
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
		return d.Dedup.Effect(ctx, engine.EffectRef{Identity: engine.EventIdentity(&c.Env), Effect: "gamble:wager"}, func(ctx context.Context) error {
			return gc.run(ctx, args, emit)
		})
	}
}

func (gc gambleCmd) run(ctx context.Context, arg string, emit module.Emit) error {
	login := strings.ToLower(gc.c.Env.ChatterUserLogin)
	viewerID, err := strconv.ParseUint(gc.c.Env.ChatterUserID, 10, 64)
	if err != nil {
		return nil
	}
	if login == "" || viewerID == 0 {
		return nil
	}
	gc.viewerID = viewerID

	bal, err := gc.d.Loyalty.BalanceGet(ctx, gc.c.BroadcasterID, gc.viewerID)
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

	allowed, err := gc.claimCooldown(ctx)
	if err != nil {
		return err
	}
	if !allowed {
		return gc.replyCooling(ctx, emit)
	}

	return gc.settle(ctx, login, wagerOutcome{bet: bet}, emit)
}

func (gc gambleCmd) replyCooling(ctx context.Context, emit module.Emit) error {
	left, err := gc.d.Cooldown.TimeLeft(ctx, gc.gambleCooldownKey())
	if err != nil {
		return err
	}
	secs := max(int64(math.Ceil(left.Seconds())), 1)
	gc.reply(emit, "", "gamble.cool", "secs", strconv.FormatInt(secs, 10))
	return nil
}

func (gc gambleCmd) settle(ctx context.Context, login string, wager wagerOutcome, emit module.Emit) error {
	roll, err := engine.RollGamble()
	if err != nil {
		gc.log.Warn("gamble: roll failed", gc.c.BID(), zap.Error(err))
		return err
	}
	wager.roll = roll
	wager.won = engine.GambleWins(roll, gc.cfg.WinPercent)
	out, err := gc.d.Loyalty.BalanceWager(ctx, engine.PointWager{BroadcasterID: gc.c.BroadcasterID, ViewerID: gc.viewerID, Login: login, Amount: wager.bet, Won: wager.won})
	if err != nil {
		gc.log.Warn("gamble: settlement failed", gc.c.BID(), zap.Error(err))
		return err
	}
	if !gc.acceptSettlement(out, emit) {
		return nil
	}
	wager.balance = out.Balance.Points
	gc.recordRound(ctx, wager)
	gc.announce(emit, wager)
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
	won     bool
}

type refusal struct {
	key    replyKey
	tokens []string
}

func (gc gambleCmd) refuse(arg string) (int64, refusal) {
	bet, outcome := parseWagerAmount(arg).wager(gc.balance, gc.cfg.MinBet, gc.cfg.MaxBet)
	switch outcome {
	case wagerOK:
		return gc.acceptBet(bet)
	case wagerInvalid:
		return 0, refusal{key: "gamble.usage"}
	case wagerBelowMin:
		return 0, gc.belowMin()
	case wagerAboveMax:
		return 0, refusal{key: "gamble.max", tokens: boundKV("max", gc.cfg.MaxBet)}
	default:
		return 0, gc.broke()
	}
}

func (gc gambleCmd) belowMin() refusal {
	if gc.balance < gc.cfg.MinBet {
		return gc.broke()
	}
	return refusal{key: "gamble.min", tokens: boundKV("min", gc.cfg.MinBet)}
}

func (gc gambleCmd) broke() refusal {
	return refusal{key: "gamble.broke", tokens: boundKV("balance", gc.balance)}
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

func (gc gambleCmd) claimCooldown(ctx context.Context) (bool, error) {
	if gc.d.Cooldown == nil || gc.cfg.CooldownSeconds <= 0 {
		return true, nil
	}
	allowed, err := gc.d.Cooldown.Allow(ctx,
		gc.gambleCooldownKey(),
		engine.GambleCooldown(gc.cfg.CooldownSeconds))
	if err != nil {
		gc.log.Warn("gamble: cooldown check failed", gc.c.BID(), zap.Error(err))
		return false, err
	}
	return allowed, nil
}

func (gc gambleCmd) announce(emit module.Emit, wager wagerOutcome) {
	override, key := gc.tmpl.LoseMessage, "gamble.lose"
	if wager.won {
		override, key = gc.tmpl.WinMessage, "gamble.win"
	}
	if wager.bet == gc.balance {
		key += ".all"
	}
	gc.reply(emit, override, replyKey(key),
		"roll", strconv.FormatInt(wager.roll, 10),
		"chance", strconv.FormatInt(gc.cfg.WinPercent, 10),
		"amount", strconv.FormatInt(wager.bet, 10),
		"balance", strconv.FormatInt(wager.balance, 10))
}

func (gc gambleCmd) gambleCooldownKey() string {
	return "games:gamble:" + strconv.FormatUint(gc.c.BroadcasterID, 10) + ":" + strconv.FormatUint(gc.viewerID, 10)
}

func (gc gambleCmd) recordRound(ctx context.Context, wager wagerOutcome) {
	end := "lost"
	if wager.won {
		end = "won"
	}
	gc.d.Analytics.AddContext(ctx, catalog.PointsFlows, 1, "gamble")
	gc.d.Analytics.AddContext(ctx, catalog.GamesRounds, 1, "gamble", end)
}
