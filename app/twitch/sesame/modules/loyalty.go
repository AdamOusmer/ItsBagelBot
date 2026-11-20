// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/i18n"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/idempotency"

	"go.uber.org/zap"
)

const loyaltyTickTimeout = 5 * time.Second

const (
	defaultLeaderboardLimit = 5
	maxLeaderboardLimit     = 10
	chatMessageMax          = 500
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
		return accrual{ev.UserID, ev.UserLogin, ev.UserName, cfg.EffectiveGiftSubPoints() * engine.TierMultiplier(ev.Tier) * int64(ev.Total)}
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
	m.Command("setpoints").Mod().Run(loyaltyRun(d, log, loyaltyCmd.setPointsRun))
	m.Command("watchtime").Everyone().Cooldown(5 * time.Second).Run(loyaltyRun(d, log, loyaltyCmd.watchtimeShow))

	m.Command("leaderboard").Everyone().Cooldown(10 * time.Second).Aliases("top").Run(loyaltyRun(d, log, loyaltyCmd.leaderboardShow))
	m.Command("freeze").Mod().Run(freezeRun(d, false))
	m.Command("unfreeze").Mod().Run(freezeRun(d, true))

	return m.Build()
}

type accrual struct {
	userID, login, name string
	points              int64
}

type pointsGiveRequest struct {
	login      string
	value      int64
	senderID   uint64
	targetID   uint64
	atPrefixed bool
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
		a.earn(ctx, d, c, cfg)
		return nil
	}
}

func onStreamTick(d engine.Deps, arm bool) module.EventHandler {
	return func(ctx context.Context, c *module.Context, _ module.Emit) error {
		if d.LoyaltyTick == nil {
			return nil
		}
		id := c.BroadcasterID
		version := c.EventVersion()
		seqOrGo(ctx, d.Seq, id, func(ctx context.Context) error {
			wctx, cancel := context.WithTimeout(ctx, loyaltyTickTimeout)
			defer cancel()
			if versioned, ok := d.LoyaltyTick.(engine.VersionedLoyaltyTicker); ok {
				if arm {
					versioned.ArmVersioned(wctx, id, version)
				} else {
					versioned.DisarmVersioned(wctx, id, version)
				}
				return nil
			}
			if arm {
				d.LoyaltyTick.Arm(wctx, id)
			} else {
				d.LoyaltyTick.Disarm(wctx, id)
			}
			return nil
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
		return fn(loyaltyCmd{newChatReplier(c), d, emit, log, cfg}, ctx, args)
	}
}

type pointsGrant struct {
	allowed func(engine.LoyaltyModuleConfig) bool
	run     func() error
}

type pointsInput struct{ target, amount string }

func (lc loyaltyCmd) pointsRun(ctx context.Context, args string) error {
	fields := strings.Fields(args)
	if len(fields) != 3 {
		return lc.pointsShow(ctx)
	}
	cfg := lc.cfg
	verb := strings.ToLower(fields[0])
	input := pointsInput{target: fields[1], amount: fields[2]}
	if verb == "give" {
		return lc.pointsGive(ctx, input, cfg.ViewersMayTransfer() || lc.owner())
	}
	grant, ok := lc.grant(ctx, verb, input)
	if !ok || !lc.c.Chatter().Allows(module.RoleModerator) {
		return lc.pointsShow(ctx)
	}
	return lc.grantVerb(grant.allowed(cfg) || lc.owner(), grant.run)
}

func (lc loyaltyCmd) setPointsRun(ctx context.Context, args string) error {
	if len(strings.Fields(args)) != 2 {
		lc.reply("loyalty.points.usage")
		return nil
	}
	return lc.pointsRun(ctx, "set "+args)
}

func (lc loyaltyCmd) grant(ctx context.Context, verb string, input pointsInput) (pointsGrant, bool) {
	switch verb {
	case "set":
		return pointsGrant{engine.LoyaltyModuleConfig.ModsMaySetPoints, func() error {
			return lc.pointsAdjust(ctx, input, true)
		}}, true
	case "add":
		return pointsGrant{engine.LoyaltyModuleConfig.ModsMayAdjustPoints, func() error {
			return lc.pointsAdjust(ctx, input, false)
		}}, true
	case "remove":
		return pointsGrant{engine.LoyaltyModuleConfig.ModsMayAdjustPoints, func() error {
			return lc.pointsRemove(ctx, input)
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
	cfg  engine.LoyaltyModuleConfig
}

func boundedAmount(amount string, positiveOnly bool) (int64, bool) {
	v, err := strconv.ParseInt(amount, 10, 64)
	if err != nil {
		return 0, false
	}
	if positiveOnly && v <= 0 {
		return 0, false
	}
	return v, true
}

func (lc loyaltyCmd) pointsAdjust(ctx context.Context, input pointsInput, absolute bool) error {
	value, ok := boundedAmount(input.amount, false)
	if !ok {
		lc.reply("loyalty.points.usage")
		return nil
	}
	login := strings.ToLower(strings.TrimPrefix(input.target, "@"))
	if login == "" {
		lc.reply("loyalty.points.usage")
		return nil
	}
	viewerID, ok := lc.resolvePointsUser(ctx, login, strings.HasPrefix(input.target, "@"))
	if !ok {
		return nil
	}
	return lc.adjustPoints(ctx, engine.BalanceAdjustment{BroadcasterID: lc.c.BroadcasterID, ViewerID: viewerID, ViewerLogin: login, Value: value, Absolute: absolute})
}

func (lc loyaltyCmd) adjustPoints(ctx context.Context, adjustment engine.BalanceAdjustment) error {
	return lc.d.Dedup.Effect(ctx, engine.EffectRef{Identity: engine.EventIdentity(&lc.c.Env), Effect: engine.EffectPointsAdjust}, func(ctx context.Context) error {
		return lc.applyPoints(ctx, adjustment)
	})
}

func (lc loyaltyCmd) applyPoints(ctx context.Context, adjustment engine.BalanceAdjustment) error {
	cfg := lc.cfg
	bal, found, err := lc.d.Loyalty.BalanceAdjustViewer(ctx, adjustment)
	if err != nil {
		lc.log.Warn("loyalty: balance adjust failed", lc.c.BID(), zap.Error(err))
		lc.pointsFailure(ctx, err)
		return err
	}
	if !found {
		lc.reply("loyalty.counter.err")
		return nil
	}
	lc.reply("loyalty.points.adjusted",
		"target", adjustment.ViewerLogin,
		"points", strconv.FormatInt(bal.Points, 10),
		"name", cfg.Name(),
	)
	lc.d.Analytics.AddContext(ctx, catalog.PointsFlows, 1, "adjust")
	return nil
}

func (lc loyaltyCmd) pointsFailure(ctx context.Context, err error) {
	idempotency.Fail(ctx, err)
	if bus.DeliveryTerminal(ctx) {
		lc.reply("loyalty.counter.err")
	}
}

func (lc loyaltyCmd) grantVerb(enabled bool, run func() error) error {
	if !enabled {
		lc.reply("loyalty.points.disabled")
		return nil
	}
	return run()
}

func (lc loyaltyCmd) pointsGive(ctx context.Context, input pointsInput, enabled bool) error {
	cfg := lc.cfg
	if !enabled {
		lc.reply("loyalty.points.disabled", "name", cfg.Name())
		return nil
	}
	req, ok := lc.pointsGiveRequest(input)
	if !ok {
		lc.reply("loyalty.points.give.usage", "name", cfg.Name())
		return nil
	}
	if req.login == strings.ToLower(lc.c.Env.ChatterUserLogin) {
		lc.reply("loyalty.points.self", "name", cfg.Name())
		return nil
	}
	targetID, ok := lc.resolvePointsUser(ctx, req.login, req.atPrefixed)
	if !ok {
		return nil
	}
	if targetID == req.senderID {
		lc.reply("loyalty.points.self", "name", cfg.Name())
		return nil
	}
	req.targetID = targetID
	return lc.transferPoints(ctx, req, cfg)
}

func (lc loyaltyCmd) resolvePointsUser(ctx context.Context, login string, atPrefixed bool) (uint64, bool) {
	if id := (viewerResolver{lc.d.TwitchAccounts, lc.log}).resolve(ctx, lc.c.Env.Mentions, viewerTarget{login, atPrefixed}); id != 0 {
		return id, true
	}
	lc.reply("loyalty.points.user.unknown", "target", login)
	return 0, false
}

func (lc loyaltyCmd) transferPoints(ctx context.Context, req pointsGiveRequest, cfg engine.LoyaltyModuleConfig) error {
	return lc.d.Dedup.Effect(ctx, engine.EffectRef{Identity: engine.EventIdentity(&lc.c.Env), Effect: engine.EffectPointsAdjust}, func(ctx context.Context) error {
		return lc.applyTransfer(ctx, req, cfg)
	})
}

func (lc loyaltyCmd) applyTransfer(ctx context.Context, req pointsGiveRequest, cfg engine.LoyaltyModuleConfig) error {
	bal, found, moved, err := lc.d.Loyalty.BalanceTransfer(ctx, engine.PointTransfer{BroadcasterID: lc.c.BroadcasterID, FromViewerID: req.senderID, TargetViewerID: req.targetID, TargetLogin: req.login, Amount: req.value})
	if err != nil {
		lc.log.Warn("loyalty: balance transfer failed", lc.c.BID(), zap.Error(err))
		lc.pointsFailure(ctx, err)
		return err
	}
	if !found {
		lc.reply("loyalty.counter.err")
		return nil
	}
	if !moved {
		lc.reply("loyalty.points.insufficient",
			"points", strconv.FormatInt(bal.Points, 10),
			"name", cfg.Name(),
		)
		return nil
	}
	lc.d.Analytics.AddContext(ctx, catalog.PointsFlows, 1, "transfer")
	lc.reply("loyalty.points.gave",
		"target", req.login,
		"amount", strconv.FormatInt(req.value, 10),
		"points", strconv.FormatInt(bal.Points, 10),
		"name", cfg.Name(),
	)
	return nil
}

func (lc loyaltyCmd) pointsGiveRequest(input pointsInput) (pointsGiveRequest, bool) {
	value, ok := boundedAmount(input.amount, true)
	if !ok {
		return pointsGiveRequest{}, false
	}
	login := strings.ToLower(strings.TrimPrefix(input.target, "@"))
	senderID, _ := strconv.ParseUint(lc.c.Env.ChatterUserID, 10, 64)
	if login == "" || senderID == 0 {
		return pointsGiveRequest{}, false
	}
	return pointsGiveRequest{login: login, value: value, senderID: senderID, atPrefixed: strings.HasPrefix(input.target, "@")}, true
}

func (lc loyaltyCmd) pointsRemove(ctx context.Context, input pointsInput) error {
	value, err := strconv.ParseInt(input.amount, 10, 64)
	if err != nil {
		lc.reply("loyalty.points.usage")
		return nil
	}
	if value > 0 {
		value = -value
	}
	input.amount = strconv.FormatInt(value, 10)
	return lc.pointsAdjust(ctx, input, false)
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
	cfg := lc.cfg
	lc.leaderboardReply(ctx, top, cfg)
	return nil
}

func (lc loyaltyCmd) leaderboardReply(ctx context.Context, top []loyaltyrpc.Balance, cfg engine.LoyaltyModuleConfig) {
	name := cfg.Name()
	link, visible := publicPageURL(ctx, lc.c, lc.d, publicPage{
		base:        lc.d.LeaderboardBaseURL,
		defaultBase: "https://leaderboard.itsbagelbot.com",
		path:        "/",
		hidden:      func(u projection.User) bool { return u.LeaderboardPageHidden },
	})
	if !visible {
		lc.reply("loyalty.leaderboard", "list", standingsLine(top, runeBudget(math.MaxInt)), "name", name)
		return
	}
	const key = "loyalty.leaderboard.link"
	frame := utf8.RuneCountInString(lc.render("", key, "list", "", "name", name, "url", link))
	lc.reply(key, "list", standingsLine(top, runeBudget(chatMessageMax-frame)), "name", name, "url", link)
}

func leaderboardLimit(args string) (int, bool) {
	args = strings.TrimSpace(args)
	if args == "" {
		return defaultLeaderboardLimit, true
	}
	n, err := strconv.Atoi(args)
	if err != nil {
		return 0, false
	}
	if n < 1 || n > maxLeaderboardLimit {
		return 0, false
	}
	return n, true
}

type runeBudget int

func standingsLine(top []loyaltyrpc.Balance, maxRunes runeBudget) string {
	var b strings.Builder
	var used runeBudget
	for i, row := range top {
		name := row.ViewerName
		if name == "" {
			name = row.ViewerLogin
		}
		entry := fmt.Sprintf("%d. %s %d", i+1, name, row.Points)
		if i > 0 {
			entry = " | " + entry
		}
		used += runeBudget(utf8.RuneCountInString(entry))
		if used > maxRunes {
			break
		}
		b.WriteString(entry)
	}
	return b.String()
}

// watchtimeShow answers "!watchtime" from the caller's existing balance.
func (lc loyaltyCmd) watchtimeShow(ctx context.Context, _ string) error {
	viewerID, err := strconv.ParseUint(lc.c.Env.ChatterUserID, 10, 64)
	if err != nil || viewerID == 0 {
		return nil
	}
	bal, err := lc.d.Loyalty.BalanceGet(ctx, lc.c.BroadcasterID, viewerID)
	if err != nil {
		lc.log.Warn("loyalty: watchtime read failed", lc.c.BID(), zap.Error(err))
		lc.reply("loyalty.counter.err")
		return nil
	}
	lc.reply("loyalty.watchtime", "duration", watchSeconds(bal.WatchSeconds).humanize(lc.c))
	return nil
}

type watchSeconds uint64

func (s watchSeconds) humanize(c *module.Context) string {
	// Convert only seconds that fit a signed nanosecond duration.
	duration := time.Duration(1<<63 - 1)
	const maxWatchSeconds = watchSeconds((1<<63 - 1) / time.Second)
	if s <= maxWatchSeconds {
		duration = time.Duration(s) * time.Second
	}
	return i18n.HumanizeDuration(c.Locale, duration)
}

func (s watchSeconds) hours() string {
	return strconv.FormatFloat(float64(s)/3600, 'f', 1, 64)
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
	cfg := lc.cfg
	lc.reply("loyalty.points",
		"points", strconv.FormatInt(bal.Points, 10),
		"name", cfg.Name(),
		"hours", watchSeconds(bal.WatchSeconds).hours(),
	)
	return nil
}

func (a accrual) earn(ctx context.Context, d engine.Deps, c *module.Context, cfg engine.LoyaltyModuleConfig) {
	viewerID, err := strconv.ParseUint(a.userID, 10, 64)
	if err != nil || viewerID == 0 {
		return
	}
	a.points = cfg.AutomaticPoints(c.BroadcasterID, viewerID, a.points)
	if a.points <= 0 {
		return
	}
	err = d.Dedup.Effect(ctx, engine.EffectRef{Identity: engine.EventIdentity(&c.Env), Effect: engine.EffectEarn}, func(ctx context.Context) error {
		d.Loyalty.Earn(engine.PointEarning{Context: ctx, BroadcasterID: c.BroadcasterID, ViewerID: viewerID, Login: a.login, Name: a.name, Points: a.points})
		return nil
	})
	idempotency.Fail(ctx, err)
}

func (lc loyaltyCmd) reply(key replyKey, kv ...string) {
	lc.chatReplier.reply(lc.emit, "", key, kv...)
}
