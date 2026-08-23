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
		d:    d,
		c:    c,
		cfg:  engine.ClampGambleSettings(raw.MinBet, raw.MaxBet, raw.WinPercent, raw.CooldownSeconds),
		tmpl: raw,
		log:  log,
	}
	if strings.TrimSpace(gc.tmpl.PointsName) == "" {
		gc.tmpl.PointsName = "points"
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

	bet, outcome := engine.ResolveGambleBet(arg, gc.balance, gc.cfg.MinBet, gc.cfg.MaxBet)
	switch outcome {
	case engine.BetOK:
	case engine.BetEmpty, engine.BetInvalid:
		gc.reply(emit, "", "gamble.usage")
		return nil
	case engine.BetBelowMin:
		gc.reply(emit, "", "gamble.min", "min", strconv.FormatInt(gc.cfg.MinBet, 10))
		return nil
	case engine.BetAboveMax:
		gc.reply(emit, "", "gamble.max", "max", strconv.FormatInt(gc.cfg.MaxBet, 10))
		return nil
	default: // BetOverBalance
		gc.reply(emit, "", "gamble.broke", "balance", strconv.FormatInt(gc.balance, 10))
		return nil
	}

	// The per-user cooldown claims only once the wager itself is real, so
	// typo spam never locks anyone out of their next honest attempt.
	if gc.d.Cooldown != nil && gc.cfg.CooldownSeconds > 0 {
		allowed, err := gc.d.Cooldown.Allow(ctx,
			gambleCooldownKey(gc.c.BroadcasterID, login),
			engine.GambleCooldown(gc.cfg.CooldownSeconds))
		if err != nil {
			gc.log.Warn("gamble: cooldown check failed", gc.bid(), zap.Error(err))
			return err
		}
		if !allowed {
			gc.reply(emit, "", "gamble.cool", "secs", strconv.FormatInt(gc.cfg.CooldownSeconds, 10))
			return nil
		}
	}

	roll := engine.RollGamble()
	if engine.GambleWins(roll, gc.cfg.WinPercent) {
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
		gc.reply(emit, gc.tmpl.WinMessage, "gamble.win",
			"roll", strconv.FormatInt(roll, 10),
			"chance", strconv.FormatInt(gc.cfg.WinPercent, 10),
			"amount", strconv.FormatInt(bet, 10),
			"balance", strconv.FormatInt(newBal.Points, 10))
		return nil
	}

	// The loss rides the conditional debit: if a racing command drained the
	// account between our read and now, spent comes back false and chat gets
	// the honest "you can't cover that" instead of a phantom loss.
	newBal, _, spent, err := gc.d.Loyalty.BalanceSpend(ctx, gc.c.BroadcasterID, login, bet)
	if err != nil {
		gc.log.Warn("gamble: loss debit failed", gc.bid(), zap.Error(err))
		return err
	}
	if !spent {
		gc.reply(emit, "", "gamble.broke", "balance", strconv.FormatInt(newBal.Points, 10))
		return nil
	}
	gc.reply(emit, gc.tmpl.LoseMessage, "gamble.lose",
		"roll", strconv.FormatInt(roll, 10),
		"chance", strconv.FormatInt(gc.cfg.WinPercent, 10),
		"amount", strconv.FormatInt(bet, 10),
		"balance", strconv.FormatInt(newBal.Points, 10))
	return nil
}

// viewerID parses the chatter's Twitch id for balance reads; chat events
// always carry one, so a zero here means an unparseable event and simply
// reads as nobody (zero balance).
func (gc gambleCmd) viewerID() uint64 {
	id, _ := strconv.ParseUint(gc.c.Env.ChatterUserID, 10, 64)
	return id
}

// reply emits one localized system line; kv are {token},value pairs, with
// {user} and the currency word always available. override is the
// broadcaster's customized template ("" for fixed system lines).
func (gc gambleCmd) reply(emit module.Emit, override, key string, kv ...string) {
	tmpl := override
	if tmpl == "" {
		tmpl = i18n.T(gc.c.Locale, key)
	}
	text := module.ExpandString(tmpl, func(k string) (string, bool) {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == k {
				return kv[i+1], true
			}
		}
		switch k {
		case "user":
			return gc.c.Env.ChatterUserLogin, true
		case "points":
			return gc.tmpl.PointsName, true
		}
		return module.ParseDynamic(k)
	})
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: gc.c.Env.BroadcasterUserID,
		Text:          text,
	})
}

func (gc gambleCmd) bid() zap.Field { return zap.Uint64("broadcaster_id", gc.c.BroadcasterID) }
