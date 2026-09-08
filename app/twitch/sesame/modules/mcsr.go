// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"go.uber.org/zap"
)

// mcsrModuleName is the ModuleView key; the console MODULE_CATALOG entry and
// the dashboard module page use the same id.
const mcsrModuleName = "mcsr"

// mcsrCooldown is the shared per-command window; gossip caches upstream
// replies (the MCSR API allows 500 requests / 10 min fleet-wide), so this only
// shields chat from spam.
const mcsrCooldown = 10 * time.Second

const (
	defaultMcsrEloTemplate     = "{player}: {elo} elo · rank #{rank} · {wins}W {losses}L this season"
	defaultMcsrSessionTemplate = "{player} this stream: {elochange} elo ({elo} now) · {wins}W {losses}L {draws}D in {matches} matches"

	// legacyMcsrSessionTemplate is the session default as it shipped before
	// {draws} existed. A blank sessionMessage already picks up whatever the
	// default currently is (the dashboard shows it as a placeholder, not a
	// seeded value — ReplyEditor.svelte), so blank configs need nothing. What
	// needs handling is a config holding this string verbatim: a broadcaster
	// who copied the old default out of the placeholder, or an import that
	// materialized it. Those would otherwise stay on the old wording, which
	// reads as a bug ("3W 4L in 8 matches", see mcsrWinLossTokens).
	//
	// Upgrading it here at read time follows the same shape as govee's
	// bindingsOf and triggers' line-format fallback: stored blobs are migrated
	// by tolerating the old value, never by rewriting a broadcaster's saved
	// text in the database — an edit is unreviewable and lossy once it has
	// overwritten text somebody authored. A template that differs by even one
	// byte is treated as authored and left exactly as written.
	legacyMcsrSessionTemplate = "{player} this stream: {elochange} elo ({elo} now) · {wins}W {losses}L in {matches} matches"

	defaultMcsrLastMatchTemplate = "{player} vs {opponent}: {result} · {time} · {seed} {structure} · {elochange} elo · {ago} ago"
	defaultMcsrRecordTemplate    = "{playera} {winsa} - {winsb} {playerb} · {played} played"
	defaultMcsrLbTemplate        = "{board}: {list}"
	defaultMcsrRaceTemplate      = "#1 {leader} ({leadertime}) · {player}: {time} (#{rank})"
	defaultMcsrPbTemplate        = "{player}: {time} ({window} PB)"

	// PaceMan-backed templates. PaceMan is a separate upstream from MCSR
	// Ranked (its own gossip provider, its own cache/rate-limit budget) but
	// the commands stay on this module: "which Minecraft player" is one
	// broadcaster setting either way.
	defaultMcsrPaceTemplate     = "{player} this session: {nethers} nethers (avg {nether}) · bastion {bastion} · fortress {fortress} · fp {firstportal} · {nph} nph"
	defaultMcsrNethersTemplate  = "{player}: {nethers} nethers this session (avg {nether}) · {nph} nph"
	defaultMcsrLastFortTemplate = "{player} last fort: nether {nether} · bastion {bastion} · fortress {fortress} · fp {firstportal} · sh {stronghold} · {ago} ago"
)

// mcsrConfig is the module's dashboard configuration. Account is the linked
// default MCSR Ranked account (blank = the broadcaster's own Twitch login).
// AccountUUID is the Mojang uuid stored next to it when the resolve succeeds,
// so Ranked/Hypixel-style lookups skip the name hop and survive a rename.
// Toggle/message semantics match the urchin module.
type mcsrConfig struct {
	// linkedAccountConfig carries account/accountUuid/linkedOnly: the linked
	// MCSR Ranked account (blank = the broadcaster's own Twitch login), the
	// Mojang uuid stored next to it when the resolve succeeds (so
	// Ranked/Hypixel-style lookups skip the name hop and survive a rename),
	// and the "only my linked account" toggle.
	linkedAccountConfig

	EloEnabled     string `json:"eloEnabled"`
	EloMessage     string `json:"eloMessage"`
	SessionEnabled string `json:"sessionEnabled"`
	SessionMessage string `json:"sessionMessage"`

	PaceEnabled     string `json:"paceEnabled"`
	PaceMessage     string `json:"paceMessage"`
	NethersEnabled  string `json:"nethersEnabled"`
	NethersMessage  string `json:"nethersMessage"`
	LastFortEnabled string `json:"lastFortEnabled"`
	LastFortMessage string `json:"lastFortMessage"`

	LastMatchEnabled string `json:"lastMatchEnabled"`
	LastMatchMessage string `json:"lastMatchMessage"`
	RecordEnabled    string `json:"recordEnabled"`
	RecordMessage    string `json:"recordMessage"`
	LbEnabled        string `json:"lbEnabled"`
	LbMessage        string `json:"lbMessage"`
	RaceEnabled      string `json:"raceEnabled"`
	RaceMessage      string `json:"raceMessage"`

	PbEnabled string `json:"pbEnabled"`
	PbMessage string `json:"pbMessage"`
}

// Mcsr owns the MCSR Ranked commands backed by the gossip service. It is a
// named, opt-in module (KindOptIn): off by default, enabled on the dashboard
// with a linked account.
//
// Commands: !elo (current rating + season record), !session (elo and record
// since the stream started). The session baseline is snapshotted when
// stream.online arrives — gossip stores the player's standing keyed by
// this channel — so "this stream" is exactly the live session's duration.
//
// !lastmatch (most recent match result), !record (head-to-head totals
// between two players) and !lb (top of the elo/phase/record leaderboards)
// round out the MCSR Ranked surface; !race answers from the separate
// weekly-race pool. !elo, !lastmatch, !record and !lb all accept a trailing
// "season:<n>" argument token (parseMcsrSeason) to look at a past season
// instead of the current one.
//
// !pb <window> [player] answers the player's PaceMan personal best for
// "daily"/"weekly"/"monthly" (an optional trailing player defaults to the
// bare-name form, e.g. "!pb Feinberg" == all-time) or the MCSR Ranked
// season-best time for "ranked". The first three windows ride PaceMan's own
// precomputed pbs object (one call, see the paceman provider); "ranked"
// answers from the mcsr provider's existing user lookup instead — it already
// fetches BestTimeMS for !elo, unused until now.
//
// !pace, !nethers and !lastfort ride the same linked account but answer
// through the paceman gossip provider instead: PaceMan.gg tracks live
// speedrun splits (nether/bastion/fortress/portal/stronghold/end), a
// different concern from MCSR Ranked's match results, so it is a separate
// upstream and a separate cache/rate-limit budget behind the same module.
//
// Handlers across both command families (see mcsr_ranked.go and
// mcsr_pace.go) share one shape: decode the config, check its toggle,
// resolve the account, call gossip, chat an upstream error, expand a
// template. That shape is statsHandler in external.go, the same one the
// valorant/clashroyale/fortnite/urchin commands ride; each command here
// supplies only what differs (which toggle, which endpoint, how to build the
// request, how to render a successful reply).
func Mcsr(d engine.Deps) module.Module {
	m := module.NewModule(mcsrModuleName, module.KindOptIn)

	m.Command("elo").Everyone().Cooldown(mcsrCooldown).Aliases("mcsr", "ranked").
		Run(mcsrEloRun(d))
	m.Command("session").Everyone().Cooldown(mcsrCooldown).Aliases("mcsrsession").
		Run(mcsrSessionRun(d))
	m.Command("lastmatch").Everyone().Cooldown(mcsrCooldown).Aliases("rankedmatch").
		Run(mcsrLastMatchRun(d))
	m.Command("record").Everyone().Cooldown(mcsrCooldown).Aliases("matchrecord").
		Run(mcsrRecordRun(d))
	m.Command("lb").Everyone().Cooldown(mcsrCooldown).Aliases("leaderboard", "rankedlb").
		Run(mcsrLbRun(d))
	m.Command("race").Everyone().Cooldown(mcsrCooldown).Aliases("weeklyrace").
		Run(mcsrRaceRun(d))
	m.Command("pb").Everyone().Cooldown(mcsrCooldown).Aliases("personalbest").
		Run(mcsrPbRun(d))
	m.Command("pace").Everyone().Cooldown(mcsrCooldown).Aliases("pacesession", "splits").
		Run(mcsrPaceRun(d))
	m.Command("nethers").Everyone().Cooldown(mcsrCooldown).Aliases("nph").
		Run(mcsrNethersRun(d))
	m.Command("lastfort").Everyone().Cooldown(mcsrCooldown).Aliases("lastpace", "previousfort").
		Run(mcsrLastFortRun(d))

	// Snapshot the linked account's standing the moment the stream goes online
	// so !session has a baseline, and clear it when the stream ends — a rapid
	// stop/restart cycle (#561) must not leave !session diffing the new stream
	// against the old one's snapshot. The pipeline only runs these for an
	// enabled module and wires the module config in, so the snapshot targets
	// the linked account.
	online, offline := snapshotHandlers(d, snapshotSpec[mcsrConfig, gossiprpc.McsrSnapshotReply]{
		provider: mcsrProvider,
		enabled:  func(mcsrConfig) bool { return true },
		request:  mcsrSnapshotRequest,
		stored:   func(r *gossiprpc.McsrSnapshotReply) zap.Field { return zap.Int("elo", r.Elo) },
	})
	m.On("stream.online", online)
	m.On("stream.offline", offline)

	return m.Build()
}

// mcsrSnapshotRequest builds the stream-start call: the linked account (never
// a typed one — nobody types at a lifecycle event) and the channel the
// baseline is filed under.
func mcsrSnapshotRequest(c *module.Context, cfg mcsrConfig, channelID string) gossiprpc.Request {
	account, _ := resolveLinked(c, accountSources{
		Linked: cfg.Account, LinkedUUID: cfg.AccountUUID, PreferUUID: true,
	})
	return gossiprpc.Request{Account: account, ChannelID: channelID, IsPremium: c.Regress.IsPremium()}
}

// mcsrRoute names one MCSR Ranked gossip endpoint, pacemanRoute one PaceMan
// endpoint. Both families sit behind this one module but are separate
// upstreams with their own cache and rate-limit budgets.
func mcsrRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: mcsrProvider, Endpoint: endpoint}
}

func pacemanRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: pacemanProvider, Endpoint: endpoint}
}

// mcsrRequest is the request strategy an mcsr command supplies. It is named
// so the handler literals below read as wiring rather than as type spelling.
type mcsrRequest = func(statsCall[mcsrConfig], statsSubject) gossiprpc.Request

// mcsrProvider and pacemanProvider name the two upstreams behind this module.
const (
	mcsrProvider    = "mcsr"
	pacemanProvider = "paceman"
)

// mcsrCommand is the wiring every mcsr command shares: the module's own
// config, the linked Minecraft account, and an account-only request. render is
// what actually differs — how one reply reads in chat — and a command whose
// request carries more than an account (a season, a window, the channel id)
// replaces that one field.
//
// It is a constructor, not a type. What it replaced was a parallel handler
// type whose hooks were handed a resolved account and nothing else, which is
// precisely what kept !record (two accounts) and !lb (no account) hand-rolling
// the whole skeleton instead of riding it. Here the strategies keep their
// shared signatures, so any command can still say what it needs to.
func mcsrCommand[R any](d engine.Deps, route engine.GossipRoute, enabled func(mcsrConfig) string, render func(statsCall[mcsrConfig], *R) string) statsHandler[mcsrConfig, R] {
	return statsHandler[mcsrConfig, R]{
		d:       d,
		enabled: enabled,
		route:   route,
		// The uuid preference follows the upstream, not the command: MCSR
		// Ranked accepts a stored Mojang uuid and it survives a rename, while
		// PaceMan is keyed by username and rejects one. Deriving it from the
		// route means a new endpoint on either side cannot get it wrong.
		target:  linkedTarget[mcsrConfig](route.Provider == mcsrProvider),
		request: accountRequest[mcsrConfig],
		render:  render,
	}
}

// mcsrSeasonRun peels the trailing "season:<n>" token off this call's typed
// args before the handler sees them — the shared account resolution reads the
// first word, and "season:3" is not a player — and hands the parsed season to
// scope, which builds that call's request.
//
// The season arrives here rather than as a field on the handler because it is
// a property of the call, not of the command: !elo and !lastmatch answer for
// whichever season the viewer typed. The handler is copied per call for the
// same reason — mutating the shared one would race every concurrent chat line.
func mcsrSeasonRun[R any](h statsHandler[mcsrConfig, R], scope func(season int) mcsrRequest) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		rest, season := parseMcsrSeason(args)
		scoped := h
		scoped.request = scope(season)
		return scoped.run(ctx, c, rest, emit)
	}
}

// mcsrAccountSeason is the request the season-scoped single-account commands
// (!elo, !lastmatch) send: the resolved account, that call's season, and the
// caller's premium lane.
func mcsrAccountSeason(season int) mcsrRequest {
	return func(call statsCall[mcsrConfig], subject statsSubject) gossiprpc.Request {
		return gossiprpc.Request{Account: subject.Account, Season: season, IsPremium: call.Ctx.Regress.IsPremium()}
	}
}

// mcsrEmpty builds the empty-state override the PaceMan commands share: a
// reply carrying no run data chats one plain translated line naming the
// player, instead of a template whose every number would render zero. empty
// reports whether this reply is that case and which player it was about.
func mcsrEmpty[R any](key string, empty func(*R) (player string, isEmpty bool)) func(statsCall[mcsrConfig], *R) (string, bool) {
	return func(call statsCall[mcsrConfig], reply *R) (string, bool) {
		player, isEmpty := empty(reply)
		if !isEmpty {
			return "", false
		}
		return mcsrEmptyText(call.Ctx, player, key), true
	}
}
