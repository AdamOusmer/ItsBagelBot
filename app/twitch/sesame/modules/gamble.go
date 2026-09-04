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
// default, armed from the loyalty page because it spends that ledger and
// cannot run while the currency is off.
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
	PointsName      string `json:"pointsName"`      // leftover in stored blobs; voice now comes from loyalty
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
// standing. ok=false means the loyalty surface is absent or the currency
// module is off (game inert).
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
		gameReplier: newGameReplier(c, points),
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
		gc.log.Warn("gamble: balance read failed", gc.bid(), zap.Error(err))
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
		gc.reply(emit, "", "gamble.cool", tk("secs", strconv.FormatInt(gc.cfg.CooldownSeconds, 10)))
		return nil
	}

	// Escrow before the dice: the conditional debit IS the wager. A win pays
	// the stake back plus its match on top; a loss keeps the debit — so no
	// sequence of overlapping commands can pay out more than was staked.
	balance, ok, err := gc.escrow(ctx, login, bet, emit)
	if err != nil || !ok {
		return err
	}
	return gc.settle(ctx, login, wagerOutcome{bet: bet, balance: balance}, emit)
}

// settle rolls the dice against the escrowed stake and answers chat.
func (gc gambleCmd) settle(ctx context.Context, login string, wager wagerOutcome, emit module.Emit) error {
	roll, err := engine.RollGamble()
	if err != nil {
		gc.log.Warn("gamble: roll failed", gc.bid(), zap.Error(err))
		return err
	}
	wager.roll = roll
	if engine.GambleWins(roll, gc.cfg.WinPercent) {
		return gc.settleWin(ctx, login, wager, emit)
	}
	gc.reply(emit, gc.tmpl.LoseMessage, "gamble.lose",
		tk("roll", strconv.FormatInt(roll, 10)),
		tk("chance", strconv.FormatInt(gc.cfg.WinPercent, 10)),
		tk("amount", strconv.FormatInt(wager.bet, 10)),
		tk("balance", strconv.FormatInt(wager.balance, 10)))
	return nil
}

// escrow takes the stake through the conditional debit. ok=false means the
// refusal has been answered (unknown viewer or short balance); err is an
// infra failure.
func (gc gambleCmd) escrow(ctx context.Context, login string, bet int64, emit module.Emit) (balance int64, ok bool, err error) {
	newBal, found, spent, err := gc.d.Loyalty.BalanceSpend(ctx, gc.c.BroadcasterID, login, bet)
	switch {
	case err != nil:
		gc.log.Warn("gamble: stake debit failed", gc.bid(), zap.Error(err))
		return 0, false, err
	case !found:
		gc.reply(emit, "", "gamble.unknown")
		return 0, false, nil
	case !spent:
		// A racing command drained the account between our read and now;
		// the service's fresh reply names what they actually hold.
		gc.reply(emit, "", "gamble.broke",
			tk("balance", strconv.FormatInt(newBal.Points, 10)))
		return 0, false, nil
	}
	return newBal.Points, true, nil
}

// wagerOutcome bundles the three numbers every settled-wager line carries:
// the dice, the stake and the post-wager standing.
type wagerOutcome struct {
	roll    int64
	bet     int64
	balance int64
}

// refusal is one rejected wager: the line to answer with and any bound
// tokens it carries. An empty key means the bet stands.
type refusal struct {
	key    replyKey
	tokens []token
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
		return 0, refusal{key: "gamble.min", tokens: boundKV("min", gc.cfg.MinBet)}
	case engine.BetAboveMax:
		return 0, refusal{key: "gamble.max", tokens: boundKV("max", gc.cfg.MaxBet)}
	default: // BetOverBalance
		return 0, refusal{key: "gamble.broke", tokens: []token{tk("balance", strconv.FormatInt(gc.balance, 10))}}
	}
}

// boundKV renders a limit line's token pair.
func boundKV(name string, limit int64) []token {
	return []token{tk(name, strconv.FormatInt(limit, 10))}
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
func (gc gambleCmd) settleWin(ctx context.Context, login string, wager wagerOutcome, emit module.Emit) error {
	// The stake is already escrowed: a win returns it with its match on top.
	newBal, found, err := gc.d.Loyalty.BalanceAdjust(ctx, gc.c.BroadcasterID, login, wager.bet*2, false)
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
	wager.balance = newBal.Points
	gc.announce(emit, gc.tmpl.WinMessage, "gamble.win", wager)
	return nil
}

// announce emits one settled-wager line; every outcome line carries the same
// four tokens (the dice and the money), only the template differs.
func (gc gambleCmd) announce(emit module.Emit, override string, key replyKey, wager wagerOutcome) {
	gc.reply(emit, override, key,
		tk("roll", strconv.FormatInt(wager.roll, 10)),
		tk("chance", strconv.FormatInt(gc.cfg.WinPercent, 10)),
		tk("amount", strconv.FormatInt(wager.bet, 10)),
		tk("balance", strconv.FormatInt(wager.balance, 10)))
}

// viewerID parses the chatter's Twitch id for balance reads; chat events
// always carry one, so a zero here means an unparseable event and simply
// reads as nobody (zero balance).
func (gc gambleCmd) viewerID() uint64 {
	id, _ := strconv.ParseUint(gc.c.Env.ChatterUserID, 10, 64)
	return id
}

func (gc gambleCmd) bid() zap.Field { return zap.Uint64("broadcaster_id", gc.c.BroadcasterID) }
