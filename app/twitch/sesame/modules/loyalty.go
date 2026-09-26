// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const loyaltyTickTimeout = 5 * time.Second

const counterAddMax = 1_000_000

const (
	defaultLeaderboardLimit = 5
	maxLeaderboardLimit     = 10
)

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

	m.On("stream.online", onStreamTick(d, true))
	m.On("stream.offline", onStreamTick(d, false))

	m.Command("points").Everyone().Cooldown(5 * time.Second).Run(loyaltyRun(d, log, loyaltyCmd.pointsRun))

	m.Command("leaderboard").Everyone().Cooldown(10 * time.Second).Run(loyaltyRun(d, log, loyaltyCmd.leaderboardShow))

	m.Command("counter").Mod().Run(loyaltyRun(d, log, loyaltyCmd.runCounter))

	return m.Build()
}

type accrual struct {
	userID, login, name string
	points              int64
}

type pointsGiveRequest struct {
	login    string
	value    int64
	senderID uint64
}

func onAccrual[T any](d engine.Deps, award func(cfg engine.LoyaltyModuleConfig, ev T) accrual) module.EventHandler {
	return func(ctx context.Context, c *module.Context, _ module.Emit) error {
		var cfg engine.LoyaltyModuleConfig
		if err := c.Decode(&cfg); err != nil {
			return err
		}
		if d.Loyalty == nil || len(c.Env.Event) == 0 {
			return nil
		}
		var ev T
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		a := award(cfg, ev)
		earn(ctx, d, c, a)
		return nil
	}
}

func onStreamTick(d engine.Deps, arm bool) module.EventHandler {
	return func(_ context.Context, c *module.Context, _ module.Emit) error {
		if d.LoyaltyTick == nil {
			return nil
		}
		id := c.BroadcasterID
		version := eventVersion(c)
		seqOrGo(d.Seq, id, d.Log, func() {
			wctx, cancel := context.WithTimeout(context.Background(), loyaltyTickTimeout)
			defer cancel()
			if versioned, ok := d.LoyaltyTick.(engine.VersionedLoyaltyTicker); ok {
				if arm {
					versioned.ArmVersioned(wctx, id, version)
				} else {
					versioned.DisarmVersioned(wctx, id, version)
				}
				return
			}
			if arm {
				d.LoyaltyTick.Arm(wctx, id)
			} else {
				d.LoyaltyTick.Disarm(wctx, id)
			}
		})
		return nil
	}
}

func loyaltyRun(d engine.Deps, log *zap.Logger, fn func(loyaltyCmd, context.Context, string) error) func(context.Context, *module.Context, string, module.Emit) error {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.Loyalty == nil {
			return nil
		}
		var cfg engine.LoyaltyModuleConfig
		if err := c.Decode(&cfg); err != nil {
			return err
		}
		return fn(loyaltyCmd{newChatReplier(c), d, emit, log}, ctx, args)
	}
}

type pointsGrant struct {
	allowed func(engine.LoyaltyModuleConfig) bool
	run     func() error
}

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
		return lc.pointsShow(ctx)
	}
	return lc.grantVerb(grant.allowed(cfg) || lc.owner(), grant.run)
}

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

func (lc loyaltyCmd) owner() bool {
	return lc.c.Env.ChatterUserID == lc.c.Env.BroadcasterUserID
}

type loyaltyCmd struct {
	chatReplier
	d    engine.Deps
	emit module.Emit
	log  *zap.Logger
}

const pointsAdjustMax = 100_000_000

func boundedAmount(amount string, positiveOnly bool) (int64, bool) {
	v, err := strconv.ParseInt(amount, 10, 64)
	if err != nil {
		return 0, false
	}
	if v > pointsAdjustMax || v < -pointsAdjustMax {
		return 0, false
	}
	if positiveOnly && v <= 0 {
		return 0, false
	}
	return v, true
}

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

	duplicate, release := lc.d.Dedup.Claim(ctx, engine.EffectRef{Identity: engine.EventIdentity(&lc.c.Env), Effect: engine.EffectPointsAdjust})
	if duplicate {
		return nil
	}
	bal, found, err := lc.d.Loyalty.BalanceAdjust(ctx, lc.c.BroadcasterID, login, value, absolute)
	if err != nil {
		release()
		lc.log.Warn("loyalty: balance adjust failed", lc.c.BID(), zap.Error(err))
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

func (lc loyaltyCmd) grantVerb(enabled bool, run func() error) error {
	if !enabled {
		lc.reply("loyalty.points.disabled")
		return nil
	}
	return run()
}

func (lc loyaltyCmd) pointsGive(ctx context.Context, target, amount string, enabled bool) error {
	var cfg engine.LoyaltyModuleConfig
	_ = lc.c.Decode(&cfg)
	if !enabled {
		lc.reply("loyalty.points.disabled", "name", cfg.Name())
		return nil
	}
	req, ok := lc.pointsGiveRequest(target, amount)
	if !ok {
		lc.reply("loyalty.points.give.usage", "name", cfg.Name())
		return nil
	}
	if req.login == strings.ToLower(lc.c.Env.ChatterUserLogin) {
		lc.reply("loyalty.points.self", "name", cfg.Name())
		return nil
	}
	duplicate, release := lc.d.Dedup.Claim(ctx, engine.EffectRef{Identity: engine.EventIdentity(&lc.c.Env), Effect: engine.EffectPointsAdjust})
	if duplicate {
		return nil
	}
	bal, found, moved, err := lc.d.Loyalty.BalanceTransfer(ctx, lc.c.BroadcasterID, req.senderID, req.login, req.value)
	if err != nil {
		release()
		lc.log.Warn("loyalty: balance transfer failed", lc.c.BID(), zap.Error(err))
		lc.reply("loyalty.counter.err")
		return nil
	}
	if !found {
		lc.reply("loyalty.points.unknown", "target", req.login)
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
		"target", req.login,
		"amount", strconv.FormatInt(req.value, 10),
		"points", strconv.FormatInt(bal.Points, 10),
		"name", cfg.Name(),
	)
	return nil
}

func (lc loyaltyCmd) pointsGiveRequest(target, amount string) (pointsGiveRequest, bool) {
	value, ok := boundedAmount(amount, true)
	if !ok {
		return pointsGiveRequest{}, false
	}
	login := strings.ToLower(strings.TrimPrefix(target, "@"))
	senderID, _ := strconv.ParseUint(lc.c.Env.ChatterUserID, 10, 64)
	if login == "" || senderID == 0 {
		return pointsGiveRequest{}, false
	}
	return pointsGiveRequest{login: login, value: value, senderID: senderID}, true
}

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

func (lc loyaltyCmd) leaderboardShow(ctx context.Context, args string) error {
	limit, ok := leaderboardLimit(args)
	if !ok {
		lc.reply("loyalty.leaderboard.usage")
		return nil
	}
	top, err := lc.d.Loyalty.Top(ctx, lc.c.BroadcasterID, limit)
	if err != nil {
		lc.log.Warn("loyalty: top read failed", lc.c.BID(), zap.Error(err))
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

func (lc loyaltyCmd) pointsShow(ctx context.Context) error {
	viewerID, err := strconv.ParseUint(lc.c.Env.ChatterUserID, 10, 64)
	if err != nil || viewerID == 0 {
		return nil
	}
	bal, err := lc.d.Loyalty.BalanceGet(ctx, lc.c.BroadcasterID, viewerID)
	if err != nil {
		lc.log.Warn("loyalty: balance read failed", lc.c.BID(), zap.Error(err))
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

func earn(ctx context.Context, d engine.Deps, c *module.Context, a accrual) {
	if a.points <= 0 {
		return
	}
	viewerID, err := strconv.ParseUint(a.userID, 10, 64)
	if err != nil || viewerID == 0 {
		return
	}
	if d.Dedup.Duplicate(ctx, engine.EffectRef{Identity: engine.EventIdentity(&c.Env), Effect: engine.EffectEarn}) {
		return
	}
	d.Loyalty.Earn(c.BroadcasterID, viewerID, a.login, a.name, a.points, 0)
}

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
		return lc.counterShow(ctx, verb, strings.Join(rest, " "))
	}
}

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

type counterAddArgs struct {
	name    string
	delta   int64
	command string
}

type counterAddLine []string

func (l counterAddLine) parse() (counterAddArgs, bool) {
	if len(l) == 0 {
		return counterAddArgs{}, false
	}
	delta, ok := l.delta()
	if !ok {
		return counterAddArgs{}, false
	}
	return counterAddArgs{name: l[0], delta: delta, command: l.bucket()}, true
}

func (l counterAddLine) delta() (int64, bool) {
	if len(l) < 2 {
		return 1, true
	}
	n, err := strconv.ParseInt(l[1], 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	if n > counterAddMax || n < -counterAddMax {
		return 0, false
	}
	return n, true
}

func (l counterAddLine) bucket() string {
	if len(l) > 2 {
		return strings.Join(l[2:], " ")
	}
	return ""
}

func (lc loyaltyCmd) counterAdd(ctx context.Context, rest []string) error {
	args, ok := counterAddLine(rest).parse()
	if !ok {
		lc.reply("loyalty.counter.usage")
		return nil
	}
	viewerID, _ := strconv.ParseUint(lc.c.Env.ChatterUserID, 10, 64)
	viewer := engine.Viewer{ID: viewerID, Login: lc.c.Env.ChatterUserLogin, Name: lc.c.Env.ChatterUserName}
	value, err := lc.d.Loyalty.CounterBump(ctx, engine.CounterBump{
		BroadcasterID: lc.c.BroadcasterID,
		Name:          args.name,
		Viewer:        viewer,
		Command:       args.command,
		Delta:         args.delta,
	})
	if errors.Is(err, engine.ErrReservedCounter) {
		lc.reply("loyalty.counter.not_found", "counter", engine.NormalizeCounterName(args.name))
		return nil
	}
	if err != nil {
		return lc.fail("add", err)
	}
	lc.reply("loyalty.counter.set",
		"counter", engine.NormalizeCounterName(args.name), "value", strconv.FormatInt(value, 10))
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
	counter, found, err := lc.d.Loyalty.CounterPeek(ctx, engine.CounterTarget{
		BroadcasterID: lc.c.BroadcasterID,
		Name:          name,
		ViewerID:      viewerID,
		Command:       command,
	})
	if err != nil {
		return lc.fail("show", err)
	}
	if !found {
		lc.reply("loyalty.counter.not_found", "counter", engine.NormalizeCounterName(name))
		return nil
	}
	key := replyKey("loyalty.counter.show")
	if counter.Scope == data.CounterScopeViewer || counter.Scope == data.CounterScopeViewerCommand {
		key = "loyalty.counter.show.viewer"
	}
	lc.reply(key, "counter", counter.Name, "value", strconv.FormatInt(counter.Value, 10))
	return nil
}

func (lc loyaltyCmd) fail(op string, err error) error {
	lc.log.Warn("loyalty: counter "+op+" failed", lc.c.BID(), zap.Error(err))
	lc.reply("loyalty.counter.err")
	return nil
}

func (lc loyaltyCmd) reply(key replyKey, kv ...string) {
	lc.chatReplier.reply(lc.emit, "", key, kv...)
}
