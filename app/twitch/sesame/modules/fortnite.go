// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"go.uber.org/zap"
)

const fortniteModuleName = "fortnite"

const fortniteCooldown = 10 * time.Second

const fortniteSnapshotTimeout = 10 * time.Second

const (
	defaultFortniteStatsTemplate   = "{player} all time: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D · solo {solowins}W / duo {duowins}W / squad {squadwins}W"
	defaultFortniteSeasonTemplate  = "{player} this season: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D · solo {solowins}W / duo {duowins}W / squad {squadwins}W"
	defaultFortniteSessionTemplate = "{player} this stream: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D"
	defaultFortniteStoreTemplate   = "Item Shop {date}: {items}"
)

const fortniteShopBudget = 380

type fortniteConfig struct {
	linkedAccountConfig

	AccountType string `json:"accountType"`

	StatsEnabled   string `json:"statsEnabled"`
	StatsMessage   string `json:"statsMessage"`
	SeasonEnabled  string `json:"seasonEnabled"`
	SeasonMessage  string `json:"seasonMessage"`
	SessionEnabled string `json:"sessionEnabled"`
	SessionMessage string `json:"sessionMessage"`
	StoreEnabled   string `json:"storeEnabled"`
	StoreMessage   string `json:"storeMessage"`
}

func Fortnite(d engine.Deps) module.Module {
	statsRun := fortniteStatsRun(d, fortniteStatsCommand{
		window:   "lifetime",
		enabled:  func(c fortniteConfig) string { return c.StatsEnabled },
		message:  func(c fortniteConfig) string { return c.StatsMessage },
		fallback: defaultFortniteStatsTemplate,
	})
	seasonRun := fortniteStatsRun(d, fortniteStatsCommand{
		window:   "season",
		enabled:  func(c fortniteConfig) string { return c.SeasonEnabled },
		message:  func(c fortniteConfig) string { return c.SeasonMessage },
		fallback: defaultFortniteSeasonTemplate,
	})
	sessionRun := fortniteSessionRun(d)
	storeRun := fortniteStoreRun(d)

	m := module.NewModule(fortniteModuleName, module.KindOptIn)
	m.Command("fn").Everyone().Cooldown(fortniteCooldown).
		Run(subDispatch(statsRun, map[string]module.RunFunc{
			"stats":   statsRun,
			"season":  seasonRun,
			"session": sessionRun,
			"store":   storeRun,
			"shop":    storeRun,
		}))
	m.Command("fnstats").Everyone().Cooldown(fortniteCooldown).Aliases("fortnitestats").
		Run(statsRun)
	m.Command("fnseason").Everyone().Cooldown(fortniteCooldown).
		Run(seasonRun)
	m.Command("fnsession").Everyone().Cooldown(fortniteCooldown).
		Run(sessionRun)
	m.Command("fnstore").Everyone().Cooldown(fortniteCooldown).Aliases("itemshop", "fnshop").
		Run(storeRun)

	online, offline := snapshotHandlers(d, snapshotSpec[fortniteConfig, gossiprpc.FortniteSnapshotReply]{
		provider: "fortnite",
		enabled:  func(cfg fortniteConfig) bool { return alertOn(cfg.SessionEnabled) },
		request:  fortniteSnapshotRequest,
		stored:   func(r *gossiprpc.FortniteSnapshotReply) zap.Field { return zap.String("player", r.Player) },
	})
	m.On("stream.online", online)
	m.On("stream.offline", offline)
	return m.Build()
}

func fortniteSnapshotRequest(c *module.Context, cfg fortniteConfig, channelID string) gossiprpc.Request {
	account := resolveAccount(accountSources{Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
	return gossiprpc.Request{Account: account, AccountType: cfg.AccountType, ChannelID: channelID, IsPremium: c.Regress.IsPremium()}
}

type fortniteStatsCommand struct {
	window   string
	enabled  func(fortniteConfig) string
	message  func(fortniteConfig) string
	fallback string
}

func fortniteStatsTokens() module.TokenExpander[gossiprpc.FortniteStatsReply] {
	type reply = gossiprpc.FortniteStatsReply
	return module.TokenExpander[reply]{
		"player":       func(r *reply) string { return r.Player },
		"window":       func(r *reply) string { return r.Window },
		"wins":         func(r *reply) string { return i64(r.Overall.Wins) },
		"matches":      func(r *reply) string { return i64(r.Overall.Matches) },
		"kills":        func(r *reply) string { return i64(r.Overall.Kills) },
		"kd":           func(r *reply) string { return trimScore(r.Overall.KD) },
		"winrate":      func(r *reply) string { return trimScore(r.Overall.WinRate) },
		"solowins":     func(r *reply) string { return i64(r.Solo.Wins) },
		"solomatches":  func(r *reply) string { return i64(r.Solo.Matches) },
		"solokd":       func(r *reply) string { return trimScore(r.Solo.KD) },
		"duowins":      func(r *reply) string { return i64(r.Duo.Wins) },
		"duomatches":   func(r *reply) string { return i64(r.Duo.Matches) },
		"duokd":        func(r *reply) string { return trimScore(r.Duo.KD) },
		"squadwins":    func(r *reply) string { return i64(r.Squad.Wins) },
		"squadmatches": func(r *reply) string { return i64(r.Squad.Matches) },
		"squadkd":      func(r *reply) string { return trimScore(r.Squad.KD) },
	}
}

func fortniteStatsRun(d engine.Deps, cmd fortniteStatsCommand) module.RunFunc {
	h := externalCommand[fortniteConfig, gossiprpc.FortniteStatsReply]{
		route:      fortniteRoute("stats"),
		enabled:    cmd.enabled,
		message:    cmd.message,
		fallback:   cmd.fallback,
		tokens:     fortniteStatsTokens(),
		preferName: true,
	}.handler(d)
	h.request = func(call statsCall[fortniteConfig], subject statsSubject) gossiprpc.Request {
		return gossiprpc.Request{
			Account:     subject.Account,
			AccountType: call.Cfg.AccountType,
			TimeWindow:  cmd.window,
			IsPremium:   call.Ctx.Regress.IsPremium(),
		}
	}
	return h.run
}

func fortniteRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: "fortnite", Endpoint: endpoint}
}

func fortniteSessionText(locale string, cfg fortniteConfig, reply *gossiprpc.FortniteSessionReply) string {
	if !reply.HasSnapshot {
		return reply.Player + ": session tracking just started, come back after a few games!"
	}
	return module.KV(
		"player", reply.Player,
		"wins", i64(reply.Wins),
		"matches", i64(reply.Matches),
		"kills", i64(reply.Kills),
		"kd", trimScore(reply.KD),
		"winrate", trimScore(reply.WinRate),
	).WithLocale(module.Locale(locale)).ExpandString(orDefault(cfg.SessionMessage, defaultFortniteSessionTemplate))
}

func fortniteSessionRun(d engine.Deps) module.RunFunc {
	h := statsHandler[fortniteConfig, gossiprpc.FortniteSessionReply]{
		d:       d,
		enabled: func(cfg fortniteConfig) string { return cfg.SessionEnabled },
		route:   fortniteRoute("session"),
		target:  linkedTarget[fortniteConfig](false),
		request: func(call statsCall[fortniteConfig], subject statsSubject) gossiprpc.Request {
			return gossiprpc.Request{
				Account:     subject.Account,
				AccountType: call.Cfg.AccountType,
				ChannelID:   strconv.FormatUint(call.Ctx.BroadcasterID, 10),
				IsPremium:   call.Ctx.Regress.IsPremium(),
			}
		},
		render: func(call statsCall[fortniteConfig], reply *gossiprpc.FortniteSessionReply) string {
			return fortniteSessionText(call.Ctx.Locale, call.Cfg, reply)
		},
	}
	return ignoreArgs(h.run)
}

func fortniteStoreRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.FortniteShopReply
	return statsHandler[fortniteConfig, reply]{
		d:       d,
		enabled: func(cfg fortniteConfig) string { return cfg.StoreEnabled },
		route:   fortniteRoute("shop"),
		target:  fixedSubject[fortniteConfig]("item shop"),
		request: accountRequest[fortniteConfig],
		render: func(call statsCall[fortniteConfig], r *reply) string {
			return module.KV(
				"date", r.Date,
				"count", strconv.Itoa(r.Count),
				"items", formatShopEntries(call.Ctx.Locale, r.Entries),
			).WithLocale(module.Locale(call.Ctx.Locale)).ExpandString(orDefault(call.Cfg.StoreMessage, defaultFortniteStoreTemplate))
		},
	}.run
}

func formatShopEntries(locale string, entries []gossiprpc.FortniteShopEntry) string {
	if len(entries) == 0 {
		return i18n.T(locale, "fortnite.shop.empty")
	}
	var b strings.Builder
	shown := 0
	for _, e := range entries {
		part := e.Name
		if e.Price > 0 {
			part += " (" + i64(e.Price) + ")"
		}
		if shown > 0 && b.Len()+len(part)+2 > fortniteShopBudget {
			break
		}
		if shown > 0 {
			b.WriteString(", ")
		}
		b.WriteString(part)
		shown++
	}
	if rest := len(entries) - shown; rest > 0 {
		b.WriteString(" " + fmt.Sprintf(i18n.T(locale, "fortnite.shop.more"), rest))
	}
	return b.String()
}
