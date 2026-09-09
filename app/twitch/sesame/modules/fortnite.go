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
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

// fortniteModuleName is the ModuleView key; the console MODULE_CATALOG entry
// and the dashboard module page use the same id.
const fortniteModuleName = "fortnite"

// fortniteCooldown is the shared per-command window; gossip caches
// upstream replies, so this only shields chat from command spam, not the API.
const fortniteCooldown = 10 * time.Second

// fortniteSnapshotTimeout bounds the fire-and-forget stream-start snapshot call.
const fortniteSnapshotTimeout = 10 * time.Second

// Default reply templates. The broadcaster customizes them per command on the
// module page; blank falls back to these.
const (
	defaultFortniteStatsTemplate   = "{player} all time: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D · solo {solowins}W / duo {duowins}W / squad {squadwins}W"
	defaultFortniteSeasonTemplate  = "{player} this season: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D · solo {solowins}W / duo {duowins}W / squad {squadwins}W"
	defaultFortniteSessionTemplate = "{player} this stream: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D"
	defaultFortniteStoreTemplate   = "Item Shop {date}: {items}"
)

// fortniteShopBudget caps the rendered {items} list so the chat line stays
// inside Twitch's 500-char message limit with room for the template around it.
const fortniteShopBudget = 380

// fortniteConfig is the module's dashboard configuration. Account is the
// linked account name (blank = the broadcaster's own Twitch login) and
// AccountType the platform namespace it lives in (epic/psn/xbl). The window
// is not configuration: !fnstats is always all-time and !season always the
// current season. The *Enabled toggles are stored "on"/"off" — empty means
// on, matching the alerts module's semantics — and each *Message is a
// customized template (blank = default).
type fortniteConfig struct {
	// linkedAccountConfig carries account/accountUuid/linkedOnly. Fortnite
	// identities are name-plus-platform, so accountUuid stays unset here and
	// is never consulted (the resolution asks for it only under PreferUUID);
	// AccountType below is the platform namespace instead.
	linkedAccountConfig

	// AccountType is the platform namespace the linked account lives in
	// (epic/psn/xbl).
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

// Fortnite owns the Fortnite chat commands backed by the gossip service. It
// is a named, opt-in module (KindOptIn): off by default, enabled on the
// dashboard, where the broadcaster links a default account. Viewers can
// always target another player explicitly: "!fn Ninja".
//
// The command surface is one root with subcommands, plus the squashed forms
// as direct triggers:
//
//	!fn [player]         all-time Battle Royale stats (also !fn stats, !fnstats)
//	!fn season [player]  the current season's stats (also !fnseason)
//	!fn session          wins/kills/K/D since the stream started (also !fnsession)
//	!fn store            the current item-shop rotation (also !fnstore)
//
// All stats replies carry the solo/duo/squad breakdown; gossip resolves
// the season window itself. The session baseline is snapshotted when
// stream.online arrives — gossip stores the linked account's standing
// keyed by this channel — so "this stream" is exactly the live session.
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

	// Snapshot the linked account's standing the moment the stream goes online,
	// so !fn session has a baseline, and clear it when the stream ends. Gated
	// on the session toggle: no point spending the tight daily stats budget for
	// a command the broadcaster turned off.
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

// fortniteSnapshotRequest builds the stream-start call: the linked account
// (never a typed one — nobody types at a lifecycle event), its platform
// namespace and the channel the baseline is filed under.
func fortniteSnapshotRequest(c *module.Context, cfg fortniteConfig, channelID string) gossiprpc.Request {
	account := resolveAccount(accountSources{Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
	return gossiprpc.Request{Account: account, AccountType: cfg.AccountType, ChannelID: channelID, IsPremium: c.Regress.IsPremium()}
}

// fortniteStatsCommand names one stats command's wiring: the fixed window it
// queries and where its toggle and template live in the config blob.
type fortniteStatsCommand struct {
	window   string
	enabled  func(fortniteConfig) string
	message  func(fortniteConfig) string
	fallback string
}

// fortniteStatsTokens is the !fnstats template palette over the gossip reply.
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

// fortniteStatsRun answers one stats command (!fn / !fnstats all-time,
// !fn season / !fnseason the current season) with the player's Battle Royale
// stats over cmd's fixed window. Template tokens: {player} {window} {wins}
// {matches} {kills} {kd}
// {winrate} plus the per-mode {solowins} {solomatches} {solokd} {duowins}
// {duomatches} {duokd} {squadwins} {squadmatches} {squadkd}.
func fortniteStatsRun(d engine.Deps, cmd fortniteStatsCommand) module.RunFunc {
	h := externalCommand[fortniteConfig, gossiprpc.FortniteStatsReply]{
		route:    fortniteRoute("stats"),
		enabled:  cmd.enabled,
		message:  cmd.message,
		fallback: cmd.fallback,
		tokens:   fortniteStatsTokens(),
		// Fortnite is name-keyed: never substitute a stored uuid.
		preferName: true,
	}.handler(d)
	// The shared account request carries no platform namespace or window, and
	// both are per-command wiring rather than something a viewer types.
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

// fortniteRoute names one Fortnite gossip endpoint.
func fortniteRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: "fortnite", Endpoint: endpoint}
}

// fortniteSessionText renders the !fn session chat line: the delta line when a
// baseline exists, otherwise a "tracking just started" note. Template tokens:
// {player} {wins} {matches} {kills} {kd} {winrate}.
func fortniteSessionText(cfg fortniteConfig, reply *gossiprpc.FortniteSessionReply) string {
	if !reply.HasSnapshot {
		return reply.Player + ": session tracking just started, come back after a few games!"
	}
	return module.ExpandString(orDefault(cfg.SessionMessage, defaultFortniteSessionTemplate), func(tok tmpl.Token) (string, bool) {
		switch tok.Key() {
		case "player":
			return reply.Player, true
		case "wins":
			return i64(reply.Wins), true
		case "matches":
			return i64(reply.Matches), true
		case "kills":
			return i64(reply.Kills), true
		case "kd":
			return trimScore(reply.KD), true
		case "winrate":
			return trimScore(reply.WinRate), true
		}
		return tmpl.Dynamic(tok)
	})
}

// fortniteSessionRun answers !fn session / !fnsession with the delta since the
// stream-start snapshot. Like the mcsr session command it always targets the
// linked account, never a typed argument: the baseline is stored per channel
// and keyed to the linked account, so honoring an arbitrary player would
// clobber the streamer's stream-start baseline. Without a baseline (module
// enabled mid-stream) gossip starts tracking now and the reply says so
// instead of faking a zero delta.
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
			return fortniteSessionText(call.Cfg, reply)
		},
	}
	return ignoreArgs(h.run)
}

// fortniteStoreRun answers !store with the current item-shop rotation.
// Template tokens: {date} {count} {items}.
func fortniteStoreRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.FortniteShopReply
	return statsHandler[fortniteConfig, reply]{
		d:       d,
		enabled: func(cfg fortniteConfig) string { return cfg.StoreEnabled },
		route:   fortniteRoute("shop"),
		// The rotation is global: no account scopes it, so a failure names the
		// feature instead of a player and the shared request sends the empty
		// account it leaves behind (the wire value is the same as omitting it).
		target:  fixedSubject[fortniteConfig]("item shop"),
		request: accountRequest[fortniteConfig],
		render: func(call statsCall[fortniteConfig], r *reply) string {
			// {items} needs the channel's locale for its "+N more" tail, which
			// a TokenExpander palette has no way to reach, so this command
			// renders its own template rather than declaring one.
			return module.ExpandString(orDefault(call.Cfg.StoreMessage, defaultFortniteStoreTemplate), func(tok tmpl.Token) (string, bool) {
				switch tok.Key() {
				case "date":
					return r.Date, true
				case "count":
					return strconv.Itoa(r.Count), true
				case "items":
					return formatShopEntries(call.Ctx.Locale, r.Entries), true
				}
				return tmpl.Dynamic(tok)
			})
		},
	}.run
}

// formatShopEntries renders the shop offers as "Name (price), ..." within the
// chat budget; whatever does not fit collapses into "+N more". Prices are
// V-Bucks; a zero price (a free or bugged offer) renders name-only.
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
