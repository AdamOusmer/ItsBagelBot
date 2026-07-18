package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// loyaltyTickTimeout bounds each fire-and-forget watch-tick arm/disarm, the
// same posture as the live module's writes.
const loyaltyTickTimeout = 5 * time.Second

// counterAddMax bounds one !counter add step so a typo cannot warp a counter
// beyond repair (set remains the unbounded escape hatch).
const counterAddMax = 1_000_000

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
// It owns two commands: !points (a viewer's own standing) and !counter (mod
// management of the named counters the {counter:...} response token and the
// channel-points bindings bump).
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

	m.Command("points").Everyone().Cooldown(5 * time.Second).Run(
		func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
			if d.Loyalty == nil {
				return nil
			}
			lc := loyaltyCmd{d, c, emit, log}
			// Mod grants: "!points set @user 500" / "!points add @user -100".
			// Gated here (not on the command) so plain "!points" stays open to
			// everyone while the mutating verbs need at least moderator.
			if fields := strings.Fields(args); len(fields) == 3 && c.Chatter().Allows(module.RoleModerator) {
				switch strings.ToLower(fields[0]) {
				case "set":
					return lc.pointsAdjust(ctx, fields[1], fields[2], true)
				case "add", "give":
					return lc.pointsAdjust(ctx, fields[1], fields[2], false)
				}
			}
			return lc.pointsShow(ctx)
		})

	m.Command("counter").Mod().Run(
		func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
			if d.Loyalty == nil {
				return nil
			}
			return loyaltyCmd{d, c, emit, log}.runCounter(ctx, args)
		})

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
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		a := award(cfg, ev)
		earn(d, c, a.userID, a.login, a.name, a.points)
		return nil
	}
}

// onStreamTick builds the stream.online/offline handlers: the watch tick
// follows the stream lifecycle, mirroring the timers module. Fire-and-forget
// on a Background-derived context, like the live module's writes.
func onStreamTick(d engine.Deps, arm bool) module.EventHandler {
	return func(_ context.Context, c *module.Context, _ module.Emit) error {
		if d.LoyaltyTick == nil {
			return nil
		}
		id := c.BroadcasterID
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), loyaltyTickTimeout)
			defer cancel()
			if arm {
				d.LoyaltyTick.Arm(wctx, id)
			} else {
				d.LoyaltyTick.Disarm(wctx, id)
			}
		}()
		return nil
	}
}

// loyaltyCmd bundles the per-invocation state the !points/!counter helpers
// share, so each helper reads as (ctx, arguments) instead of a six-way
// parameter list — the same shape as the queue module's queueCmd.
type loyaltyCmd struct {
	d    engine.Deps
	c    *module.Context
	emit module.Emit
	log  *zap.Logger
}

// pointsAdjustMax bounds one mod grant so a typo cannot warp a balance beyond
// repair.
const pointsAdjustMax = 100_000_000

// pointsAdjust runs one mod grant: "!points set/add @user <n>". The target is
// addressed by login; the loyalty service resolves it against the balances it
// has seen, so a viewer with no accrual yet cannot be granted (they get a row
// the moment they chat while live, sub, or cheer).
func (lc loyaltyCmd) pointsAdjust(ctx context.Context, target, amount string, absolute bool) error {
	value, err := strconv.ParseInt(amount, 10, 64)
	if err != nil || value > pointsAdjustMax || value < -pointsAdjustMax {
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

// counterCreate makes a counter one of the three ways, all per channel:
// "!counter create <name>" a single global value, "... <name> user" one value
// per viewer, "... <name> user+command" one value per viewer per command.
func (lc loyaltyCmd) counterCreate(ctx context.Context, rest []string) error {
	if len(rest) == 0 {
		lc.reply("loyalty.counter.usage")
		return nil
	}
	scope := data.CounterScopeChannel
	if len(rest) > 1 {
		switch strings.ToLower(rest[1]) {
		case "user", "viewer", "per-viewer", "perviewer":
			scope = data.CounterScopeViewer
		case "user+command", "user-command", "usercommand", "viewer+command", "command":
			scope = data.CounterScopeViewerCommand
		}
	}
	counter, err := lc.d.Loyalty.CounterCreate(ctx, lc.c.BroadcasterID, rest[0], scope)
	if err != nil {
		return lc.fail("create", err)
	}
	lc.reply("loyalty.counter.created", "counter", counter.Name, "scope", scopeLabel(counter.Scope))
	return nil
}

// scopeLabel is the chat-facing name of a scope.
func scopeLabel(scope string) string {
	switch scope {
	case data.CounterScopeViewer:
		return "per user"
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
	value, err := lc.d.Loyalty.CounterBump(ctx, lc.c.BroadcasterID, rest[0], viewerID, command, delta)
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
		case data.CounterScopeViewer:
			b.WriteString(" (per user)")
		case data.CounterScopeViewerCommand:
			b.WriteString(" (per user+command)")
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
	if counter.Scope != data.CounterScopeChannel {
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
