// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
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

// mcsrSnapshotTimeout bounds the fire-and-forget stream-start snapshot call.
const mcsrSnapshotTimeout = 10 * time.Second

const (
	defaultMcsrEloTemplate     = "{player}: {elo} elo · rank #{rank} · {wins}W {losses}L this season"
	defaultMcsrSessionTemplate = "{player} this stream: {elochange} elo ({elo} now) · {wins}W {losses}L in {matches} matches"

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
// Toggle/message semantics match the urchin module.
type mcsrConfig struct {
	Account string `json:"account"`

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
// template. mcsrHandler.run (below) is that shape's one implementation;
// each command supplies only what differs (which toggle, which endpoint,
// how to build the request, how to render a successful reply).
func Mcsr(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(mcsrModuleName, module.KindOptIn)

	m.Command("session").Everyone().Cooldown(mcsrCooldown).Aliases("mcsrsession").
		Run(mcsrSessionRun(d))
	m.Command("record").Everyone().Cooldown(mcsrCooldown).Aliases("matchrecord").
		Run(mcsrRecordRun(d))
	m.Command("lb").Everyone().Cooldown(mcsrCooldown).Aliases("leaderboard", "rankedlb").
		Run(mcsrLbRun(d))
	m.Command("race").Everyone().Cooldown(mcsrCooldown).Aliases("weeklyrace").
		Run(mcsrRaceRun(d))
	m.Command("pb").Everyone().Cooldown(mcsrCooldown).Aliases("personalbest").
		Run(mcsrPbRun(d))

	for _, reg := range mcsrSeasonCommands(d) {
		m.Command(reg.name).Everyone().Cooldown(mcsrCooldown).Aliases(reg.aliases...).Run(reg.run)
	}
	for _, reg := range mcsrPaceCommands(d) {
		m.Command(reg.name).Everyone().Cooldown(mcsrCooldown).Aliases(reg.aliases...).Run(reg.run)
	}

	// Snapshot the linked account's standing the moment the stream goes online.
	// The pipeline only runs this when the module is enabled, and it wires the
	// module config in, so the snapshot targets the linked account. Fire and
	// forget on a Background-derived context (the consumer's ctx is acked and
	// may cancel the moment the handler returns), mirroring the live module's
	// write discipline.
	m.On("stream.online", func(_ context.Context, c *module.Context, _ module.Emit) error {
		if d.Gossip == nil {
			return nil
		}
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		account := resolveAccount(accountSources{Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		channelID := strconv.FormatUint(c.BroadcasterID, 10)
		seqOrGo(d.Seq, c.BroadcasterID, log, func() {
			wctx, cancel := context.WithTimeout(context.Background(), mcsrSnapshotTimeout)
			defer cancel()
			var reply gossiprpc.McsrSnapshotReply
			if err := d.Gossip.Call(wctx, engine.GossipRoute{Provider: "mcsr", Endpoint: "session_start"}, gossiprpc.Request{Account: account, ChannelID: channelID, IsPremium: c.Regress.IsPremium()}, &reply); err != nil {
				log.Warn("mcsr: stream-start snapshot failed",
					zap.String("channel_id", channelID), zap.String("account", account), zap.Error(err))
				return
			}
			log.Debug("mcsr: stream-start snapshot stored",
				zap.String("channel_id", channelID), zap.String("account", account), zap.Int("elo", reply.Elo))
		})
		return nil
	})

	// Stream ended: clear the session-start baseline so a rapid stop/restart
	// cycle (#561) cannot leave !session diffing the new stream against the old
	// one's snapshot. Sequenced behind the online snapshot like every other
	// lifecycle effect. Gossip deployments without the provider (or in shop-only
	// mode) answer no-responder — an expected miss, hence Debug.
	m.On("stream.offline", func(_ context.Context, c *module.Context, _ module.Emit) error {
		if d.Gossip == nil {
			return nil
		}
		channelID := strconv.FormatUint(c.BroadcasterID, 10)
		seqOrGo(d.Seq, c.BroadcasterID, log, func() {
			wctx, cancel := context.WithTimeout(context.Background(), mcsrSnapshotTimeout)
			defer cancel()
			var reply gossiprpc.McsrSnapshotReply
			if err := d.Gossip.Call(wctx, engine.GossipRoute{Provider: "mcsr", Endpoint: "session_end"}, gossiprpc.Request{ChannelID: channelID}, &reply); err != nil {
				log.Debug("mcsr: stream-end snapshot clear failed",
					zap.String("channel_id", channelID), zap.Error(err))
			}
		})
		return nil
	})

	return m.Build()
}

// mcsrHandler is the shape every !mcsr and !pace command shares: decode the
// module config, check its toggle, resolve the account, call gossip, chat
// an upstream error, and — on success — render a reply into chat text. Each
// command builds one of these (see mcsr_ranked.go / mcsr_pace.go) instead of
// copy-pasting that sequence, which is what CodeScene's duplication finding
// flagged.
type mcsrHandler[R any] struct {
	d engine.Deps

	// enabled reads the command's own toggle field off the decoded config.
	enabled func(mcsrConfig) string
	// route names the gossip provider/endpoint this command calls.
	route engine.GossipRoute
	// request builds the gossip request once the account is resolved.
	request func(c *module.Context, account string, cfg mcsrConfig) gossiprpc.Request
	// reply turns a successful gossip reply into the chat line to send.
	reply func(c *module.Context, cfg mcsrConfig, reply R) string
}

// run implements module.RunFunc's signature, so most commands can hand it
// straight to Command(...).Run without an extra wrapper closure; commands
// that need to pre-process their typed args (peeling off a "season:<n>" or
// window token) call it directly with the trimmed args instead.
func (h mcsrHandler[R]) run(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
	var cfg mcsrConfig
	_ = c.Decode(&cfg)
	if !alertOn(h.enabled(cfg)) || h.d.Gossip == nil {
		return nil
	}

	account := resolveAccount(accountSources{Arg: args, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
	var reply R
	if err := h.d.Gossip.Call(ctx, h.route, h.request(c, account, cfg), &reply); err != nil {
		if chatReplyError(c, emit, account, err) {
			return nil
		}
		return err
	}

	mcsrEmit(c, emit, h.reply(c, cfg, reply))
	return nil
}

// mcsrEmit sends text as a chat Output — the one shape every !mcsr/!pace
// reply resolves to, whether it came from an expanded template or a plain
// i18n line.
func mcsrEmit(c *module.Context, emit module.Emit, text string) {
	emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text})
}

// mcsrSimpleRequest builds a gossip request carrying only the resolved
// account and the caller's premium lane — the shape every account-only
// !mcsr/!pace command (pace, nethers, lastfort, race, !pb ranked) shares.
// Commands that need extra fields (season, a channel id, a time window)
// build their own request instead of using this one.
func mcsrSimpleRequest(c *module.Context, account string, _ mcsrConfig) gossiprpc.Request {
	return gossiprpc.Request{Account: account, IsPremium: c.Regress.IsPremium()}
}

// mcsrSeasonRequest builds the request-building closure the season-scoped
// commands (!elo, !lastmatch) share: same fields, a different season parsed
// from that call's typed args.
func mcsrSeasonRequest(season int) func(c *module.Context, account string, cfg mcsrConfig) gossiprpc.Request {
	return func(c *module.Context, account string, _ mcsrConfig) gossiprpc.Request {
		return gossiprpc.Request{Account: account, Season: season, IsPremium: c.Regress.IsPremium()}
	}
}

// mcsrSeasonSpec is what a season-scoped command (!elo, !lastmatch) supplies
// to mcsrSeasonCommand: its own toggle, endpoint, optional "no data yet"
// check and message/template/tokens. Season itself is threaded in
// separately since it comes from that call's typed args, not from the
// command's fixed wiring.
type mcsrSeasonSpec[R any] struct {
	enabled  func(mcsrConfig) string
	endpoint string
	// isEmpty is nil for commands that never special-case an empty reply
	// (e.g. !elo). When set and it reports empty, mcsrEmptyText(player, key)
	// is sent instead of expanding the template.
	isEmpty  func(R) (player, key string, empty bool)
	message  func(mcsrConfig) string
	template string
	tokens   func(c *module.Context, reply R) func(string) (string, bool)
}

// mcsrSeasonCommand builds the mcsrHandler shared by every !mcsr command
// that accepts a trailing "season:<n>" token: same provider, a request
// carrying that call's parsed season, everything else supplied by spec.
func mcsrSeasonCommand[R any](d engine.Deps, season int, spec mcsrSeasonSpec[R]) mcsrHandler[R] {
	return mcsrHandler[R]{
		d:       d,
		enabled: spec.enabled,
		route:   engine.GossipRoute{Provider: "mcsr", Endpoint: spec.endpoint},
		request: mcsrSeasonRequest(season),
		reply: func(c *module.Context, cfg mcsrConfig, reply R) string {
			if spec.isEmpty != nil {
				if player, key, empty := spec.isEmpty(reply); empty {
					return mcsrEmptyText(c, player, key)
				}
			}
			tmpl := orDefault(spec.message(cfg), spec.template)
			return module.ExpandString(tmpl, spec.tokens(c, reply))
		},
	}
}

// mcsrSeasonRunFunc wraps a season-scoped command's spec into a
// module.RunFunc: peel the trailing "season:<n>" token off that call's typed
// args, build the handler for the parsed season and dispatch. Factoring this
// out means !elo and !lastmatch don't each carry their own copy of this
// three-line shape.
func mcsrSeasonRunFunc[R any](d engine.Deps, spec mcsrSeasonSpec[R]) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		rest, season := parseMcsrSeason(args)
		h := mcsrSeasonCommand(d, season, spec)
		return h.run(ctx, c, rest, emit)
	}
}

// mcsrCommandReg is one command's Twitch-facing wiring: its name, aliases
// and the RunFunc that answers it. Building a command family (see
// mcsrPaceCommands, mcsrSeasonCommands) from a table of these means Mcsr
// doesn't carry one near-identical "spec -> handler -> Command(...).Run(...)"
// line per command.
type mcsrCommandReg struct {
	name    string
	aliases []string
	run     module.RunFunc
}
