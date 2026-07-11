package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/i18n"
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

	m.On("channel.subscribe", func(_ context.Context, c *module.Context, _ module.Emit) error {
		var cfg engine.LoyaltyModuleConfig
		_ = c.Decode(&cfg)
		if d.Loyalty == nil || len(c.Env.Event) == 0 {
			return nil
		}
		var ev loyaltySubEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		earn(d, c, ev.UserID, ev.UserLogin, ev.UserName, cfg.EffectiveSubPoints()*engine.TierMultiplier(ev.Tier))
		return nil
	})

	m.On("channel.subscription.message", func(_ context.Context, c *module.Context, _ module.Emit) error {
		var cfg engine.LoyaltyModuleConfig
		_ = c.Decode(&cfg)
		if d.Loyalty == nil || len(c.Env.Event) == 0 {
			return nil
		}
		var ev loyaltySubEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		earn(d, c, ev.UserID, ev.UserLogin, ev.UserName, cfg.EffectiveResubPoints()*engine.TierMultiplier(ev.Tier))
		return nil
	})

	m.On("channel.subscription.gift", func(_ context.Context, c *module.Context, _ module.Emit) error {
		var cfg engine.LoyaltyModuleConfig
		_ = c.Decode(&cfg)
		if d.Loyalty == nil || len(c.Env.Event) == 0 {
			return nil
		}
		var ev loyaltyGiftEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.IsAnonymous || ev.Total <= 0 {
			return nil // nobody to credit; recipients earn via their own channel.subscribe
		}
		earn(d, c, ev.UserID, ev.UserLogin, ev.UserName, cfg.EffectiveGiftSubPoints()*int64(ev.Total))
		return nil
	})

	m.On("channel.cheer", func(_ context.Context, c *module.Context, _ module.Emit) error {
		var cfg engine.LoyaltyModuleConfig
		_ = c.Decode(&cfg)
		if d.Loyalty == nil || len(c.Env.Event) == 0 {
			return nil
		}
		var ev loyaltyCheerEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.IsAnonymous || ev.Bits <= 0 {
			return nil
		}
		earn(d, c, ev.UserID, ev.UserLogin, ev.UserName, cfg.EffectiveCheerPointsPer100()*int64(ev.Bits)/100)
		return nil
	})

	// The watch tick follows the stream lifecycle, mirroring the timers module:
	// online arms it (the clock re-checks the module's enable state itself),
	// offline stops it immediately.
	m.On("stream.online", func(_ context.Context, c *module.Context, _ module.Emit) error {
		if d.LoyaltyTick == nil {
			return nil
		}
		id := c.BroadcasterID
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), loyaltyTickTimeout)
			defer cancel()
			d.LoyaltyTick.Arm(wctx, id)
		}()
		return nil
	})

	m.On("stream.offline", func(_ context.Context, c *module.Context, _ module.Emit) error {
		if d.LoyaltyTick == nil {
			return nil
		}
		id := c.BroadcasterID
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), loyaltyTickTimeout)
			defer cancel()
			d.LoyaltyTick.Disarm(wctx, id)
		}()
		return nil
	})

	m.Command("points").Everyone().Cooldown(5 * time.Second).Run(
		func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
			if d.Loyalty == nil {
				return nil
			}
			// Mod grants: "!points set @user 500" / "!points add @user -100".
			// Gated here (not on the command) so plain "!points" stays open to
			// everyone while the mutating verbs need at least moderator.
			if fields := strings.Fields(args); len(fields) == 3 && c.Chatter().Allows(module.RoleModerator) {
				switch strings.ToLower(fields[0]) {
				case "set":
					return pointsAdjust(ctx, d, c, fields[1], fields[2], true, emit, log)
				case "add", "give":
					return pointsAdjust(ctx, d, c, fields[1], fields[2], false, emit, log)
				}
			}
			viewerID, err := strconv.ParseUint(c.Env.ChatterUserID, 10, 64)
			if err != nil || viewerID == 0 {
				return nil
			}
			bal, err := d.Loyalty.BalanceGet(ctx, c.BroadcasterID, viewerID)
			if err != nil {
				log.Warn("loyalty: balance read failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
				return nil
			}
			var cfg engine.LoyaltyModuleConfig
			_ = c.Decode(&cfg)
			loyaltyReply(c, emit, "loyalty.points",
				"points", strconv.FormatInt(bal.Points, 10),
				"name", cfg.Name(),
				"hours", strconv.FormatFloat(float64(bal.WatchSeconds)/3600, 'f', 1, 64),
			)
			return nil
		})

	m.Command("counter").Mod().Run(
		func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
			if d.Loyalty == nil {
				return nil
			}
			return runCounterCommand(ctx, d, c, args, emit, log)
		})

	return m.Build()
}

// pointsAdjustMax bounds one mod grant so a typo cannot warp a balance beyond
// repair.
const pointsAdjustMax = 100_000_000

// pointsAdjust runs one mod grant: "!points set/add @user <n>". The target is
// addressed by login; the loyalty service resolves it against the balances it
// has seen, so a viewer with no accrual yet cannot be granted (they get a row
// the moment they chat while live, sub, or cheer).
func pointsAdjust(ctx context.Context, d engine.Deps, c *module.Context, target, amount string, absolute bool, emit module.Emit, log *zap.Logger) error {
	value, err := strconv.ParseInt(amount, 10, 64)
	if err != nil || value > pointsAdjustMax || value < -pointsAdjustMax {
		loyaltyReply(c, emit, "loyalty.points.usage")
		return nil
	}
	login := strings.ToLower(strings.TrimPrefix(target, "@"))
	if login == "" {
		loyaltyReply(c, emit, "loyalty.points.usage")
		return nil
	}
	var cfg engine.LoyaltyModuleConfig
	_ = c.Decode(&cfg)

	bal, found, err := d.Loyalty.BalanceAdjust(ctx, c.BroadcasterID, login, value, absolute)
	if err != nil {
		log.Warn("loyalty: balance adjust failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		loyaltyReply(c, emit, "loyalty.counter.err")
		return nil
	}
	if !found {
		loyaltyReply(c, emit, "loyalty.points.unknown", "target", login)
		return nil
	}
	loyaltyReply(c, emit, "loyalty.points.adjusted",
		"target", login,
		"points", strconv.FormatInt(bal.Points, 10),
		"name", cfg.Name(),
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
func runCounterCommand(ctx context.Context, d engine.Deps, c *module.Context, args string, emit module.Emit, log *zap.Logger) error {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		loyaltyReply(c, emit, "loyalty.counter.usage")
		return nil
	}

	verb := strings.ToLower(fields[0])
	rest := fields[1:]
	switch verb {
	case "create":
		return counterCreate(ctx, d, c, rest, emit, log)
	case "add":
		return counterAdd(ctx, d, c, rest, emit, log)
	case "set":
		return counterSet(ctx, d, c, rest, emit, log)
	case "reset":
		return counterReset(ctx, d, c, rest, emit, log)
	case "delete", "del", "remove":
		return counterDelete(ctx, d, c, rest, emit, log)
	case "list":
		return counterListCmd(ctx, d, c, emit, log)
	default:
		// "!counter <name> [source...]": the optional trailing words select a
		// viewer+command counter's bucket — a command trigger or a
		// channel-point reward title (which may span several words).
		return counterShow(ctx, d, c, verb, strings.Join(rest, " "), emit, log)
	}
}

// counterCreate makes a counter one of the three ways, all per channel:
// "!counter create <name>" a single global value, "... <name> user" one value
// per viewer, "... <name> user+command" one value per viewer per command.
func counterCreate(ctx context.Context, d engine.Deps, c *module.Context, rest []string, emit module.Emit, log *zap.Logger) error {
	if len(rest) == 0 {
		loyaltyReply(c, emit, "loyalty.counter.usage")
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
	counter, err := d.Loyalty.CounterCreate(ctx, c.BroadcasterID, rest[0], scope)
	if err != nil {
		return counterFail(c, emit, log, "create", err)
	}
	loyaltyReply(c, emit, "loyalty.counter.created", "counter", counter.Name, "scope", scopeLabel(counter.Scope))
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

func counterAdd(ctx context.Context, d engine.Deps, c *module.Context, rest []string, emit module.Emit, log *zap.Logger) error {
	if len(rest) == 0 {
		loyaltyReply(c, emit, "loyalty.counter.usage")
		return nil
	}
	delta := int64(1)
	if len(rest) > 1 {
		n, err := strconv.ParseInt(rest[1], 10, 64)
		if err != nil || n == 0 || n > counterAddMax || n < -counterAddMax {
			loyaltyReply(c, emit, "loyalty.counter.usage")
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
	viewerID, _ := strconv.ParseUint(c.Env.ChatterUserID, 10, 64)
	value, err := d.Loyalty.CounterBump(ctx, c.BroadcasterID, rest[0], viewerID, command, delta)
	if err != nil {
		return counterFail(c, emit, log, "add", err)
	}
	loyaltyReply(c, emit, "loyalty.counter.set",
		"counter", engine.NormalizeCounterName(rest[0]), "value", strconv.FormatInt(value, 10))
	return nil
}

func counterSet(ctx context.Context, d engine.Deps, c *module.Context, rest []string, emit module.Emit, log *zap.Logger) error {
	if len(rest) < 2 {
		loyaltyReply(c, emit, "loyalty.counter.usage")
		return nil
	}
	value, err := strconv.ParseInt(rest[1], 10, 64)
	if err != nil {
		loyaltyReply(c, emit, "loyalty.counter.usage")
		return nil
	}
	found, err := d.Loyalty.CounterSet(ctx, c.BroadcasterID, rest[0], 0, "", value)
	if err != nil {
		return counterFail(c, emit, log, "set", err)
	}
	if !found {
		loyaltyReply(c, emit, "loyalty.counter.not_found", "counter", engine.NormalizeCounterName(rest[0]))
		return nil
	}
	loyaltyReply(c, emit, "loyalty.counter.set",
		"counter", engine.NormalizeCounterName(rest[0]), "value", strconv.FormatInt(value, 10))
	return nil
}

func counterReset(ctx context.Context, d engine.Deps, c *module.Context, rest []string, emit module.Emit, log *zap.Logger) error {
	if len(rest) == 0 {
		loyaltyReply(c, emit, "loyalty.counter.usage")
		return nil
	}
	found, err := d.Loyalty.CounterSet(ctx, c.BroadcasterID, rest[0], 0, "", 0)
	if err != nil {
		return counterFail(c, emit, log, "reset", err)
	}
	if !found {
		loyaltyReply(c, emit, "loyalty.counter.not_found", "counter", engine.NormalizeCounterName(rest[0]))
		return nil
	}
	loyaltyReply(c, emit, "loyalty.counter.reset", "counter", engine.NormalizeCounterName(rest[0]))
	return nil
}

func counterDelete(ctx context.Context, d engine.Deps, c *module.Context, rest []string, emit module.Emit, log *zap.Logger) error {
	if len(rest) == 0 {
		loyaltyReply(c, emit, "loyalty.counter.usage")
		return nil
	}
	if err := d.Loyalty.CounterDelete(ctx, c.BroadcasterID, rest[0]); err != nil {
		return counterFail(c, emit, log, "delete", err)
	}
	loyaltyReply(c, emit, "loyalty.counter.deleted", "counter", engine.NormalizeCounterName(rest[0]))
	return nil
}

func counterListCmd(ctx context.Context, d engine.Deps, c *module.Context, emit module.Emit, log *zap.Logger) error {
	counters, err := d.Loyalty.CounterList(ctx, c.BroadcasterID)
	if err != nil {
		return counterFail(c, emit, log, "list", err)
	}
	if len(counters) == 0 {
		loyaltyReply(c, emit, "loyalty.counter.list.empty")
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
	loyaltyReply(c, emit, "loyalty.counter.list", "list", b.String())
	return nil
}

func counterShow(ctx context.Context, d engine.Deps, c *module.Context, name, command string, emit module.Emit, log *zap.Logger) error {
	viewerID, _ := strconv.ParseUint(c.Env.ChatterUserID, 10, 64)
	counter, found, err := d.Loyalty.CounterPeek(ctx, c.BroadcasterID, name, viewerID, command)
	if err != nil {
		return counterFail(c, emit, log, "show", err)
	}
	if !found {
		loyaltyReply(c, emit, "loyalty.counter.not_found", "counter", engine.NormalizeCounterName(name))
		return nil
	}
	key := "loyalty.counter.show"
	if counter.Scope != data.CounterScopeChannel {
		key = "loyalty.counter.show.viewer"
	}
	loyaltyReply(c, emit, key, "counter", counter.Name, "value", strconv.FormatInt(counter.Value, 10))
	return nil
}

// counterFail logs the failure and posts the generic error line; the error is
// swallowed (the pipeline would only drop it anyway).
func counterFail(c *module.Context, emit module.Emit, log *zap.Logger, op string, err error) error {
	log.Warn("loyalty: counter "+op+" failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
	loyaltyReply(c, emit, "loyalty.counter.err")
	return nil
}

// loyaltyReply emits one localized chat line. kv are {token},value pairs;
// {user} (the invoking chatter) and the dynamic vars are always available.
func loyaltyReply(c *module.Context, emit module.Emit, key string, kv ...string) {
	text := module.ExpandString(i18n.T(c.Locale, key), func(k string) (string, bool) {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == k {
				return kv[i+1], true
			}
		}
		if k == "user" {
			return c.Env.ChatterUserLogin, true
		}
		return module.ParseDynamic(k)
	})
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          text,
	})
}
