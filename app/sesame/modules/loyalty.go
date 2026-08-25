// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// loyaltyTickTimeout bounds each fire-and-forget watch-tick arm/disarm, the
// same posture as the live module's writes.
const loyaltyTickTimeout = 5 * time.Second

// counterAddMax bounds one !counter add step so a typo cannot warp a counter
// beyond repair (set remains the unbounded escape hatch).
const counterAddMax = 1_000_000

// Leaderboard sizes for "!leaderboard [n]": five by default, ten at most —
// chat lines are short, and the web page carries the long form.
const (
	defaultLeaderboardLimit = 5
	maxLeaderboardLimit     = 10
)

// Event subsets. Only the fields the accrual math needs; the broadcaster id
// comes from the Context.
type loyaltySubEvent struct {
	UserID    string `json:"user_id"`
	UserLogin string `json:"user_login"`
	UserName  string `json:"user_name"`
	Tier      string `json:"tier"`
	IsGift    bool   `json:"is_gift"`
}

type loyaltyGiftEvent struct {
	IsAnonymous bool   `json:"is_anonymous"`
	UserID      string `json:"user_id"`
	UserLogin   string `json:"user_login"`
	UserName    string `json:"user_name"`
	Tier        string `json:"tier"`
	Total       int    `json:"total"`
}

type loyaltyCheerEvent struct {
	IsAnonymous bool   `json:"is_anonymous"`
	UserID      string `json:"user_id"`
	UserLogin   string `json:"user_login"`
	UserName    string `json:"user_name"`
	Bits        int    `json:"bits"`
}

// Loyalty is the channel points-and-viewtime economy. It is a named, opt-in
// module (KindOptIn): subs, resubs, gift subs and cheers award points at the
// configured rates, and while the stream is live a watch tick (see
// engine.ValkeyLoyaltyClock) awards points plus watch time to everyone in
// chat. Storage lives in the loyalty service; every accrual here is a
// fire-and-forget hand-off to the worker-side reporter.
//
// It owns three commands: !points (a viewer's own standing, plus the
// broadcaster-configurable grant and transfer verbs), !leaderboard (the
// channel's top standings) and !counter (mod management of the named counters
// the {counter:...} response token and the channel-points bindings bump).
func Loyalty(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(engine.LoyaltyModuleName, module.KindOptIn)

	m.On("channel.subscribe", onAccrual(d, func(cfg engine.LoyaltyModuleConfig, ev loyaltySubEvent) accrual {
		return accrual{ev.UserID, ev.UserLogin, ev.UserName, cfg.EffectiveSubPoints() * engine.TierMultiplier(ev.Tier)}
	}))

	m.On("channel.subscription.message", onAccrual(d, func(cfg engine.LoyaltyModuleConfig, ev loyaltySubEvent) accrual {
		return accrual{ev.UserID, ev.UserLogin, ev.UserName, cfg.EffectiveResubPoints() * engine.TierMultiplier(ev.Tier)}
	}))

	// Gift recipients earn through their own channel.subscribe events; this
	// credits the gifter (an anonymous one has nobody to credit).
	m.On("channel.subscription.gift", onAccrual(d, func(cfg engine.LoyaltyModuleConfig, ev loyaltyGiftEvent) accrual {
		if ev.IsAnonymous || ev.Total <= 0 {
			return accrual{}
		}
		return accrual{ev.UserID, ev.UserLogin, ev.UserName, cfg.EffectiveGiftSubPoints() * int64(ev.Total)}
	}))

	m.On("channel.cheer", onAccrual(d, func(cfg engine.LoyaltyModuleConfig, ev loyaltyCheerEvent) accrual {
		if ev.IsAnonymous || ev.Bits <= 0 {
			return accrual{}
		}
		return accrual{ev.UserID, ev.UserLogin, ev.UserName, cfg.EffectiveCheerPointsPer100() * int64(ev.Bits) / 100}
	}))

	// The watch tick follows the stream lifecycle, mirroring the timers module:
	// online arms it (the clock re-checks the module's enable state itself),
	// offline stops it immediately.
	m.On("stream.online", onStreamTick(d, true))
	m.On("stream.offline", onStreamTick(d, false))

	m.Command("points").Everyone().Cooldown(5 * time.Second).Run(loyaltyRun(d, log, loyaltyCmd.pointsRun))

	m.Command("leaderboard").Everyone().Cooldown(10 * time.Second).Run(loyaltyRun(d, log, loyaltyCmd.leaderboardShow))

	m.Command("counter").Mod().Run(loyaltyRun(d, log, loyaltyCmd.runCounter))

	return m.Build()
}

// accrual is one event's award: who earns and how much. A zero value (no
// viewer or nothing earned) is dropped by earn.
type accrual struct {
	userID, login, name string
	points              int64
}

// onAccrual builds the shared event-handler shell for every point source:
// decode the module config and the event subset, ask award what it is worth,
// and hand the result to the store. The per-event logic shrinks to the award
// closure.
func onAccrual[T any](d engine.Deps, award func(cfg engine.LoyaltyModuleConfig, ev T) accrual) module.EventHandler {
	return func(_ context.Context, c *module.Context, _ module.Emit) error {
		var cfg engine.LoyaltyModuleConfig
		_ = c.Decode(&cfg)
		if d.Loyalty == nil || len(c.Env.Event) == 0 {
			return nil
		}
		var ev T
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		a := award(cfg, ev)
		earn(d, c, a.userID, a.login, a.name, a.points)
		return nil
	}
}

// onStreamTick builds the stream.online/offline handlers: the watch tick
// follows the stream lifecycle, mirroring the timers module. Fire-and-forget
// on a Background-derived context, like the live module's writes, and run
// through d.Seq like them so an offline's Disarm cannot race past an online's
// Arm on this replica (#561).
func onStreamTick(d engine.Deps, arm bool) module.EventHandler {
	return func(_ context.Context, c *module.Context, _ module.Emit) error {
		if d.LoyaltyTick == nil {
			return nil
		}
		id := c.BroadcasterID
		seqOrGo(d.Seq, id, d.Log, func() {
			wctx, cancel := context.WithTimeout(context.Background(), loyaltyTickTimeout)
			defer cancel()
			if arm {
				d.LoyaltyTick.Arm(wctx, id)
			} else {
				d.LoyaltyTick.Disarm(wctx, id)
			}
		})
		return nil
	}
}

// loyaltyCmd bundles the per-invocation state the !points/!counter helpers
// share, so each helper reads as (ctx, arguments) instead of a six-way
// parameter list — the same shape as the queue module's queueCmd.
// loyaltyRun adapts a loyaltyCmd method into a command handler. All three
// commands open the same way — bail when the loyalty client is absent, then
// bind the per-invocation context — and spelling that out inline meant every
// command body carried two branches before reaching its own logic.
func loyaltyRun(d engine.Deps, log *zap.Logger, fn func(loyaltyCmd, context.Context, string) error) func(context.Context, *module.Context, string, module.Emit) error {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.Loyalty == nil {
			return nil
		}
		return fn(loyaltyCmd{d, c, emit, log}, ctx, args)
	}
}

// pointsGrant is one moderator grant verb: the capability that gates it and
// the write it performs. Table-driven because set/add/remove differ only in
// those two, and as switch cases each one repeated the moderator check and the
// capability check around its own body.
type pointsGrant struct {
	allowed func(engine.LoyaltyModuleConfig) bool
	run     func() error
}

// pointsRun routes "!points <verb> <target> <amount>". "give" moves the
// chatter's OWN points and belongs to everyone; set/add/remove are mod grants,
// gated here rather than on the command so a plain "!points" stays open to
// everyone — with per-verb toggles from the module config so a broadcaster can
// hand moderators exactly as much power as they want.
func (lc loyaltyCmd) pointsRun(ctx context.Context, args string) error {
	fields := strings.Fields(args)
	if len(fields) != 3 {
		return lc.pointsShow(ctx)
	}
	var cfg engine.LoyaltyModuleConfig
	_ = lc.c.Decode(&cfg)
	verb := strings.ToLower(fields[0])
	if verb == "give" {
		return lc.pointsGive(ctx, fields[1], fields[2], cfg.ViewersMayTransfer() || lc.owner())
	}
	grant, ok := lc.grant(ctx, verb, fields[1], fields[2])
	if !ok || !lc.c.Chatter().Allows(module.RoleModerator) {
		// A non-mod typing a grant verb (or anything unparseable) just gets
		// their own standing — never an error, never a hint of a gate.
		return lc.pointsShow(ctx)
	}
	return lc.grantVerb(grant.allowed(cfg) || lc.owner(), grant.run)
}

// grant maps a grant verb onto its capability and its write. ok=false for
// anything that is not one.
func (lc loyaltyCmd) grant(ctx context.Context, verb, target, amount string) (pointsGrant, bool) {
	switch verb {
	case "set":
		return pointsGrant{engine.LoyaltyModuleConfig.ModsMaySetPoints, func() error {
			return lc.pointsAdjust(ctx, target, amount, true)
		}}, true
	case "add":
		return pointsGrant{engine.LoyaltyModuleConfig.ModsMayAdjustPoints, func() error {
			return lc.pointsAdjust(ctx, target, amount, false)
		}}, true
	case "remove":
		return pointsGrant{engine.LoyaltyModuleConfig.ModsMayAdjustPoints, func() error {
			return lc.pointsRemove(ctx, target, amount)
		}}, true
	}
	return pointsGrant{}, false
}

// owner reports whether the caller owns the channel. The owner outranks every
// capability toggle: those gate what the owner delegates to moderators, never
// what the owner can do.
func (lc loyaltyCmd) owner() bool {
	return lc.c.Env.ChatterUserID == lc.c.Env.BroadcasterUserID
}

type loyaltyCmd struct {
	d    engine.Deps
	c    *module.Context
	emit module.Emit
	log  *zap.Logger
}

// pointsAdjustMax bounds one mod grant so a typo cannot warp a balance beyond
// repair.
const pointsAdjustMax = 100_000_000

// boundedAmount parses a grant or transfer amount, bounded by pointsAdjustMax
// so a typo cannot warp a balance beyond repair. positiveOnly is the transfer
// rule: a viewer moves points they hold, so a negative "gift" that would debit
// the recipient is not a transfer. Shared because the grant and transfer verbs
// were each spelling the parse and the bound out as one four-operand
// conditional.
func boundedAmount(amount string, positiveOnly bool) (int64, bool) {
	v, err := strconv.ParseInt(amount, 10, 64)
	if err != nil || v > pointsAdjustMax || v < -pointsAdjustMax {
		return 0, false
	}
	if positiveOnly && v <= 0 {
		return 0, false
	}
	return v, true
}

// pointsAdjust runs one mod grant: "!points set/add @user <n>". The target is
// addressed by login; the loyalty service resolves it against the balances it
// has seen, so a viewer with no accrual yet cannot be granted (they get a row
// the moment they chat while live, sub, or cheer).
func (lc loyaltyCmd) pointsAdjust(ctx context.Context, target, amount string, absolute bool) error {
	value, ok := boundedAmount(amount, false)
	if !ok {
		lc.reply("loyalty.points.usage")
		return nil
	}
	login := strings.ToLower(strings.TrimPrefix(target, "@"))
	if login == "" {
		lc.reply("loyalty.points.usage")
		return nil
	}
	var cfg engine.LoyaltyModuleConfig
	_ = lc.c.Decode(&cfg)

	bal, found, err := lc.d.Loyalty.BalanceAdjust(ctx, lc.c.BroadcasterID, login, value, absolute)
	if err != nil {
		lc.log.Warn("loyalty: balance adjust failed", zap.Uint64("broadcaster_id", lc.c.BroadcasterID), zap.Error(err))
		lc.reply("loyalty.counter.err")
		return nil
	}
	if !found {
		lc.reply("loyalty.points.unknown", "target", login)
		return nil
	}
	lc.reply("loyalty.points.adjusted",
		"target", login,
		"points", strconv.FormatInt(bal.Points, 10),
		"name", cfg.Name(),
	)
	return nil
}

// grantVerb runs one enabled mod grant; a moderator whose channel switched
// this capability off gets the denial line instead. (A non-mod never gets
// here — the dispatcher routes them to their own standing.)
func (lc loyaltyCmd) grantVerb(enabled bool, run func() error) error {
	if !enabled {
		lc.reply("loyalty.points.disabled")
		return nil
	}
	return run()
}

// pointsGive moves the chatter's OWN points to someone else ("!points give
// @user 500" — StreamElements-parity transfers, available to mods and viewers
// alike while the channel keeps them enabled). The service debits the sender
// under a points >= amount guard and credits the recipient in the same
// transaction, so a refused move leaves both balances untouched.
func (lc loyaltyCmd) pointsGive(ctx context.Context, target, amount string, enabled bool) error {
	var cfg engine.LoyaltyModuleConfig
	_ = lc.c.Decode(&cfg)
	if !enabled {
		lc.reply("loyalty.points.disabled", "name", cfg.Name())
		return nil
	}
	value, ok := boundedAmount(amount, true)
	if !ok {
		lc.reply("loyalty.points.give.usage", "name", cfg.Name())
		return nil
	}
	login := strings.ToLower(strings.TrimPrefix(target, "@"))
	senderID, _ := strconv.ParseUint(lc.c.Env.ChatterUserID, 10, 64)
	if login == "" || senderID == 0 {
		lc.reply("loyalty.points.give.usage", "name", cfg.Name())
		return nil
	}
	if login == strings.ToLower(lc.c.Env.ChatterUserLogin) {
		lc.reply("loyalty.points.self", "name", cfg.Name())
		return nil
	}
	bal, found, moved, err := lc.d.Loyalty.BalanceTransfer(ctx, lc.c.BroadcasterID, senderID, login, value)
	if err != nil {
		lc.log.Warn("loyalty: balance transfer failed", zap.Uint64("broadcaster_id", lc.c.BroadcasterID), zap.Error(err))
		lc.reply("loyalty.counter.err")
		return nil
	}
	if !found {
		lc.reply("loyalty.points.unknown", "target", login)
		return nil
	}
	if !moved {
		lc.reply("loyalty.points.insufficient",
			"points", strconv.FormatInt(bal.Points, 10),
			"name", cfg.Name(),
		)
		return nil
	}
	lc.reply("loyalty.points.gave",
		"target", login,
		"amount", strconv.FormatInt(value, 10),
		"points", strconv.FormatInt(bal.Points, 10),
		"name", cfg.Name(),
	)
	return nil
}

// pointsRemove subtracts from a viewer's balance ("!points remove @user 100"),
// the positive-amount spelling of "!points add @user -100". The negation of a
// MinInt64-sized typo wraps to itself, which pointsAdjust's bound then
// rejects as usage — so the overflow never reaches the ledger.
func (lc loyaltyCmd) pointsRemove(ctx context.Context, target, amount string) error {
	value, err := strconv.ParseInt(amount, 10, 64)
	if err != nil {
		lc.reply("loyalty.points.usage")
		return nil
	}
	if value > 0 {
		value = -value
	}
	return lc.pointsAdjust(ctx, target, strconv.FormatInt(value, 10), false)
}

// leaderboardShow answers "!leaderboard [n]": the channel's top standings by
// {name}, names bare so a spammy invocation cannot ping half the roster.
func (lc loyaltyCmd) leaderboardShow(ctx context.Context, args string) error {
	limit, ok := leaderboardLimit(args)
	if !ok {
		lc.reply("loyalty.leaderboard.usage")
		return nil
	}
	top, err := lc.d.Loyalty.Top(ctx, lc.c.BroadcasterID, limit)
	if err != nil {
		lc.log.Warn("loyalty: top read failed", zap.Uint64("broadcaster_id", lc.c.BroadcasterID), zap.Error(err))
		lc.reply("loyalty.counter.err")
		return nil
	}
	if len(top) == 0 {
		lc.reply("loyalty.leaderboard.empty")
		return nil
	}
	var cfg engine.LoyaltyModuleConfig
	_ = lc.c.Decode(&cfg)
	lc.reply("loyalty.leaderboard", "list", standingsLine(top), "name", cfg.Name())
	return nil
}

// leaderboardLimit reads the optional "[n]". ok=false is a usage error, kept
// distinct from the empty argument that means "use the default".
func leaderboardLimit(args string) (int, bool) {
	args = strings.TrimSpace(args)
	if args == "" {
		return defaultLeaderboardLimit, true
	}
	n, err := strconv.Atoi(args)
	if err != nil || n < 1 || n > maxLeaderboardLimit {
		return 0, false
	}
	return n, true
}

// standingsLine renders the one-line chat form. Names are bare — never an @ —
// so a spammy invocation cannot ping half the roster.
func standingsLine(top []loyaltyrpc.Balance) string {
	var b strings.Builder
	for i, row := range top {
		if i > 0 {
			b.WriteString(" | ")
		}
		name := row.ViewerName
		if name == "" {
			name = row.ViewerLogin
		}
		fmt.Fprintf(&b, "%d. %s %d", i+1, name, row.Points)
	}
	return b.String()
}

// pointsShow answers a plain "!points": the caller's own standing.
func (lc loyaltyCmd) pointsShow(ctx context.Context) error {
	viewerID, err := strconv.ParseUint(lc.c.Env.ChatterUserID, 10, 64)
	if err != nil || viewerID == 0 {
		return nil
	}
	bal, err := lc.d.Loyalty.BalanceGet(ctx, lc.c.BroadcasterID, viewerID)
	if err != nil {
		lc.log.Warn("loyalty: balance read failed", zap.Uint64("broadcaster_id", lc.c.BroadcasterID), zap.Error(err))
		return nil
	}
	var cfg engine.LoyaltyModuleConfig
	_ = lc.c.Decode(&cfg)
	lc.reply("loyalty.points",
		"points", strconv.FormatInt(bal.Points, 10),
		"name", cfg.Name(),
		"hours", strconv.FormatFloat(float64(bal.WatchSeconds)/3600, 'f', 1, 64),
	)
	return nil
}

// earn parses the event's viewer identity and hands the accrual to the store.
// A non-positive award (a source switched off, a sub-100-bit cheer at low
// rates) is skipped before it can publish an empty entry.
func earn(d engine.Deps, c *module.Context, userID, login, name string, points int64) {
	if points <= 0 {
		return
	}
	viewerID, err := strconv.ParseUint(userID, 10, 64)
	if err != nil || viewerID == 0 {
		return
	}
	d.Loyalty.Earn(c.BroadcasterID, viewerID, login, name, points, 0)
}

// runCounterCommand routes "!counter ..." — a bare name shows it, the
// management verbs mutate through the loyalty service.
func (lc loyaltyCmd) runCounter(ctx context.Context, args string) error {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		lc.reply("loyalty.counter.usage")
		return nil
	}

	verb := strings.ToLower(fields[0])
	rest := fields[1:]
	switch verb {
	case "create":
		return lc.counterCreate(ctx, rest)
	case "add":
		return lc.counterAdd(ctx, rest)
	case "set":
		return lc.counterSet(ctx, rest)
	case "reset":
		return lc.counterReset(ctx, rest)
	case "delete", "del", "remove":
		return lc.counterDelete(ctx, rest)
	case "list":
		return lc.counterList(ctx)
	default:
		// "!counter <name> [source...]": the optional trailing words select a
		// viewer+command counter's bucket — a command trigger or a
		// channel-point reward title (which may span several words).
		return lc.counterShow(ctx, verb, strings.Join(rest, " "))
	}
}

// counterCreate makes a counter one of the four channel ways:
// "!counter create <name>" a single global value, "... <name> user" one value
// per viewer, "... <name> command" one pooled value per command/reward,
// "... <name> user+command" one value per viewer per command. Bot-scope
// counters are admin-only and cannot be created from chat.
func (lc loyaltyCmd) counterCreate(ctx context.Context, rest []string) error {
	if len(rest) == 0 {
		lc.reply("loyalty.counter.usage")
		return nil
	}
	scope := data.CounterScopeChannel
	if len(rest) > 1 {
		scope = createScope(rest[1])
	}
	counter, err := lc.d.Loyalty.CounterCreate(ctx, lc.c.BroadcasterID, rest[0], scope)
	if err != nil {
		return lc.fail("create", err)
	}
	lc.reply("loyalty.counter.created", "counter", counter.Name, "scope", scopeLabel(counter.Scope))
	return nil
}

// createScope maps a "!counter create <name> <word>" scope word; anything
// unrecognized (or absent) is a channel counter. "command" used to mean
// user+command; it now means the pooled per-command scope, matching the
// dashboard's naming.
func createScope(word string) string {
	switch strings.ToLower(word) {
	case "user", "viewer", "per-viewer", "perviewer":
		return data.CounterScopeViewer
	case "command", "per-command", "percommand", "reward":
		return data.CounterScopeCommand
	case "user+command", "user-command", "usercommand", "viewer+command":
		return data.CounterScopeViewerCommand
	default:
		return data.CounterScopeChannel
	}
}

// scopeLabel is the chat-facing name of a scope.
func scopeLabel(scope string) string {
	switch scope {
	case data.CounterScopeViewer:
		return "per user"
	case data.CounterScopeCommand:
		return "per command"
	case data.CounterScopeViewerCommand:
		return "per user+command"
	default:
		return "channel"
	}
}

func (lc loyaltyCmd) counterAdd(ctx context.Context, rest []string) error {
	if len(rest) == 0 {
		lc.reply("loyalty.counter.usage")
		return nil
	}
	delta := int64(1)
	if len(rest) > 1 {
		n, err := strconv.ParseInt(rest[1], 10, 64)
		if err != nil || n == 0 || n > counterAddMax || n < -counterAddMax {
			lc.reply("loyalty.counter.usage")
			return nil
		}
		delta = n
	}
	// A viewer+command counter's manual add can name the bucket (a command
	// trigger or a multi-word reward title) after the value; without one it
	// lands in the empty bucket.
	command := ""
	if len(rest) > 2 {
		command = strings.Join(rest[2:], " ")
	}
	viewerID, _ := strconv.ParseUint(lc.c.Env.ChatterUserID, 10, 64)
	viewer := engine.Viewer{ID: viewerID, Login: lc.c.Env.ChatterUserLogin, Name: lc.c.Env.ChatterUserName}
	value, err := lc.d.Loyalty.CounterBump(ctx, engine.CounterBump{
		BroadcasterID: lc.c.BroadcasterID,
		Name:          rest[0],
		Viewer:        viewer,
		Command:       command,
		Delta:         delta,
	})
	if err != nil {
		return lc.fail("add", err)
	}
	lc.reply("loyalty.counter.set",
		"counter", engine.NormalizeCounterName(rest[0]), "value", strconv.FormatInt(value, 10))
	return nil
}

func (lc loyaltyCmd) counterSet(ctx context.Context, rest []string) error {
	if len(rest) < 2 {
		lc.reply("loyalty.counter.usage")
		return nil
	}
	value, err := strconv.ParseInt(rest[1], 10, 64)
	if err != nil {
		lc.reply("loyalty.counter.usage")
		return nil
	}
	found, err := lc.d.Loyalty.CounterSet(ctx, lc.c.BroadcasterID, rest[0], 0, "", value)
	if err != nil {
		return lc.fail("set", err)
	}
	if !found {
		lc.reply("loyalty.counter.not_found", "counter", engine.NormalizeCounterName(rest[0]))
		return nil
	}
	lc.reply("loyalty.counter.set",
		"counter", engine.NormalizeCounterName(rest[0]), "value", strconv.FormatInt(value, 10))
	return nil
}

func (lc loyaltyCmd) counterReset(ctx context.Context, rest []string) error {
	if len(rest) == 0 {
		lc.reply("loyalty.counter.usage")
		return nil
	}
	found, err := lc.d.Loyalty.CounterSet(ctx, lc.c.BroadcasterID, rest[0], 0, "", 0)
	if err != nil {
		return lc.fail("reset", err)
	}
	if !found {
		lc.reply("loyalty.counter.not_found", "counter", engine.NormalizeCounterName(rest[0]))
		return nil
	}
	lc.reply("loyalty.counter.reset", "counter", engine.NormalizeCounterName(rest[0]))
	return nil
}

func (lc loyaltyCmd) counterDelete(ctx context.Context, rest []string) error {
	if len(rest) == 0 {
		lc.reply("loyalty.counter.usage")
		return nil
	}
	if err := lc.d.Loyalty.CounterDelete(ctx, lc.c.BroadcasterID, rest[0]); err != nil {
		return lc.fail("delete", err)
	}
	lc.reply("loyalty.counter.deleted", "counter", engine.NormalizeCounterName(rest[0]))
	return nil
}

func (lc loyaltyCmd) counterList(ctx context.Context) error {
	counters, err := lc.d.Loyalty.CounterList(ctx, lc.c.BroadcasterID)
	if err != nil {
		return lc.fail("list", err)
	}
	if len(counters) == 0 {
		lc.reply("loyalty.counter.list.empty")
		return nil
	}
	var b strings.Builder
	for i, counter := range counters {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(counter.Name)
		switch counter.Scope {
		case data.CounterScopeViewer, data.CounterScopeCommand, data.CounterScopeViewerCommand:
			b.WriteString(" (" + scopeLabel(counter.Scope) + ")")
		default:
			b.WriteString(" (")
			b.WriteString(strconv.FormatInt(counter.Value, 10))
			b.WriteString(")")
		}
	}
	lc.reply("loyalty.counter.list", "list", b.String())
	return nil
}

func (lc loyaltyCmd) counterShow(ctx context.Context, name, command string) error {
	viewerID, _ := strconv.ParseUint(lc.c.Env.ChatterUserID, 10, 64)
	counter, found, err := lc.d.Loyalty.CounterPeek(ctx, lc.c.BroadcasterID, name, viewerID, command)
	if err != nil {
		return lc.fail("show", err)
	}
	if !found {
		lc.reply("loyalty.counter.not_found", "counter", engine.NormalizeCounterName(name))
		return nil
	}
	key := "loyalty.counter.show"
	if counter.Scope == data.CounterScopeViewer || counter.Scope == data.CounterScopeViewerCommand {
		key = "loyalty.counter.show.viewer"
	}
	lc.reply(key, "counter", counter.Name, "value", strconv.FormatInt(counter.Value, 10))
	return nil
}

// fail logs the failure and posts the generic error line; the error is
// swallowed (the pipeline would only drop it anyway).
func (lc loyaltyCmd) fail(op string, err error) error {
	lc.log.Warn("loyalty: counter "+op+" failed", zap.Uint64("broadcaster_id", lc.c.BroadcasterID), zap.Error(err))
	lc.reply("loyalty.counter.err")
	return nil
}

// reply emits one localized chat line. kv are {token},value pairs; {user}
// (the invoking chatter) and the dynamic vars are always available.
func (lc loyaltyCmd) reply(key string, kv ...string) {
	text := module.ExpandString(i18n.T(lc.c.Locale, key), func(k string) (string, bool) {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == k {
				return kv[i+1], true
			}
		}
		if k == "user" {
			return lc.c.Env.ChatterUserLogin, true
		}
		return module.ParseDynamic(k)
	})
	lc.emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: lc.c.Env.BroadcasterUserID,
		Text:          text,
	})
}
