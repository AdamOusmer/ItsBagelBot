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

// gambleModuleName is the ModuleView key; the console MODULE_CATALOG entry and
// the dashboard module page use the same id.
const gambleModuleName = "gamble"

// gambleCooldownKey is the per-user cooldown key shape. The engine's command
// cooldown is deliberately left unset on !gamble: it gates per channel, which
// would let one viewer's wager freeze everyone else's. The games gate per
// user instead, through this key claimed via the shared cooldown store.
func gambleCooldownKey(broadcasterID uint64, login string) string {
	return "games:gamble:" + strconv.FormatUint(broadcasterID, 10) + ":" + login
}

// Gamble owns "!gamble <amount|half|all>": a viewer stakes loyalty points on
// a roll, winning the stake back plus its match when the roll lands inside
// the channel's win chance. A named opt-in module (KindOptIn): off by
// default, enabled on the dashboard next to the loyalty module it plays for.
//
//	!gamble           → usage
//	!gamble <n>       → wager n points
//	!gamble half|all  → wager half of / the whole standing
//
// Every point movement is service-side: wins credit through the same adjust
// path as a mod grant, losses ride balance.spend's conditional debit, so a
// wager can never leave a negative balance behind even when two of a
// viewer's commands race.
func Gamble(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(gambleModuleName, module.KindOptIn)
	m.Command("gamble").Everyone().Run(gambleRun(d, log))
	return m.Build()
}

// gambleConfig holds the broadcaster-tunable knobs and the two customizable
// conversational replies. Numbers clamp through engine.ClampGambleSettings;
// template overrides fall back to their localized defaults when empty.
type gambleConfig struct {
	MinBet          int64  `json:"minBet"`          // default 1
	MaxBet          int64  `json:"maxBet"`          // default 1000
	WinPercent      int64  `json:"winPercent"`      // default 50 (a fair coin)
	CooldownSeconds int64  `json:"cooldownSeconds"` // default 10
	PointsName      string `json:"pointsName"`      // currency word in the win/lose lines
	WinMessage      string `json:"winMessage"`      // i18n gamble.win
	LoseMessage     string `json:"loseMessage"`     // i18n gamble.lose
}

// gambleCmd bundles the per-invocation state every handler shares.
type gambleCmd struct {
	gameReplier
	d       engine.Deps
	c       *module.Context
	cfg     engine.GambleSettings
	tmpl    gambleConfig
	balance int64
	log     *zap.Logger
}

// newGambleCmd decodes the broadcaster's config and reads the chatter's
// standing. ok=false means the loyalty surface is absent (module inert).
func newGambleCmd(d engine.Deps, c *module.Context, log *zap.Logger) (gc gambleCmd, ok bool) {
	if d.Loyalty == nil {
		return gambleCmd{}, false
	}
	var raw gambleConfig
	_ = c.Decode(&raw)
	gc = gambleCmd{
		gameReplier: newGameReplier(c, raw.PointsName),
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
		gc, ok := newGambleCmd(d, c, log)
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
		gc.log.Warn("gamble: balance read failed", gc.bid(), zap.Error(err))
		return err
	}
	gc.balance = bal.Points

	bet, refused := gc.refuse(arg)
	if refused.key != "" {
		gc.reply(emit, "", refused.key, refused.kv...)
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

	roll := engine.RollGamble()
	if engine.GambleWins(roll, gc.cfg.WinPercent) {
		return gc.settleWin(ctx, login, bet, roll, emit)
	}
	return gc.settleLoss(ctx, login, bet, roll, emit)
}

// refusal is one rejected wager: the i18n key to answer with and any bound
// tokens it carries. Empty outcome means the bet stands.
type refusal struct {
	key string
	kv  []string
}

// refuse maps the parsed wager against the channel's limits and the
// chatter's standing; every outcome but BetOK answers with itself.
func (gc gambleCmd) refuse(arg string) (int64, refusal) {
	bet, outcome := engine.ResolveGambleBet(arg, gc.balance, gc.cfg.MinBet, gc.cfg.MaxBet)
	switch outcome {
	case engine.BetOK:
		return bet, refusal{}
	case engine.BetEmpty, engine.BetInvalid:
		return 0, refusal{key: "gamble.usage"}
	case engine.BetBelowMin:
		return 0, refusal{key: "gamble.min", kv: boundKV("min", gc.cfg.MinBet)}
	case engine.BetAboveMax:
		return 0, refusal{key: "gamble.max", kv: boundKV("max", gc.cfg.MaxBet)}
	default: // BetOverBalance
		return 0, refusal{key: "gamble.broke", kv: []string{"balance", strconv.FormatInt(gc.balance, 10)}}
	}
}

// boundKV renders a limit line's token pair.
func boundKV(name string, limit int64) []string {
	return []string{name, strconv.FormatInt(limit, 10)}
}

// claimCooldown takes the chatter's per-user window once the wager itself is
// real — typo spam never locks anyone out of their next honest attempt.
func (gc gambleCmd) claimCooldown(ctx context.Context, login string) (bool, error) {
	if gc.d.Cooldown == nil || gc.cfg.CooldownSeconds <= 0 {
		return true, nil
	}
	allowed, err := gc.d.Cooldown.Allow(ctx,
		gambleCooldownKey(gc.c.BroadcasterID, login),
		engine.GambleCooldown(gc.cfg.CooldownSeconds))
	if err != nil {
		gc.log.Warn("gamble: cooldown check failed", gc.bid(), zap.Error(err))
		return false, err
	}
	return allowed, nil
}

// settleWin credits the stake back plus its match and announces.
func (gc gambleCmd) settleWin(ctx context.Context, login string, bet, roll int64, emit module.Emit) error {
	newBal, found, err := gc.d.Loyalty.BalanceAdjust(ctx, gc.c.BroadcasterID, login, bet, false)
	if err != nil {
		gc.log.Warn("gamble: win credit failed", gc.bid(), zap.Error(err))
		return err
	}
	if !found {
		// The balance read above saw them; a vanished row mid-wager is a
		// loyalty-service hiccup, not something chat should see a lie for.
		gc.reply(emit, "", "gamble.err")
		return nil
	}
	gc.announce(emit, gc.tmpl.WinMessage, "gamble.win", roll, bet, newBal.Points)
	return nil
}

// settleLoss rides the conditional debit: if a racing command drained the
// account between our read and now, spent comes back false and chat gets the
// honest "you can't cover that" instead of a phantom loss.
func (gc gambleCmd) settleLoss(ctx context.Context, login string, bet, roll int64, emit module.Emit) error {
	newBal, _, spent, err := gc.d.Loyalty.BalanceSpend(ctx, gc.c.BroadcasterID, login, bet)
	if err != nil {
		gc.log.Warn("gamble: loss debit failed", gc.bid(), zap.Error(err))
		return err
	}
	if !spent {
		gc.reply(emit, "", "gamble.broke",
			"balance", strconv.FormatInt(newBal.Points, 10))
		return nil
	}
	gc.announce(emit, gc.tmpl.LoseMessage, "gamble.lose", roll, bet, newBal.Points)
	return nil
}

// announce emits one settled-wager line; every outcome line carries the same
// four tokens (the dice and the money), only the template differs.
func (gc gambleCmd) announce(emit module.Emit, override, key string, roll, bet, balance int64) {
	gc.reply(emit, override, key,
		"roll", strconv.FormatInt(roll, 10),
		"chance", strconv.FormatInt(gc.cfg.WinPercent, 10),
		"amount", strconv.FormatInt(bet, 10),
		"balance", strconv.FormatInt(balance, 10))
}

// viewerID parses the chatter's Twitch id for balance reads; chat events
// always carry one, so a zero here means an unparseable event and simply
// reads as nobody (zero balance).
func (gc gambleCmd) viewerID() uint64 {
	id, _ := strconv.ParseUint(gc.c.Env.ChatterUserID, 10, 64)
	return id
}

func (gc gambleCmd) bid() zap.Field { return zap.Uint64("broadcaster_id", gc.c.BroadcasterID) }
