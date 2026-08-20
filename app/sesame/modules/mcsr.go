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
	"ItsBagelBot/internal/domain/i18n"
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
// !pace, !nethers and !lastfort ride the same linked account but answer
// through the paceman gossip provider instead: PaceMan.gg tracks live
// speedrun splits (nether/bastion/fortress/portal/stronghold/end), a
// different concern from MCSR Ranked's match results, so it is a separate
// upstream and a separate cache/rate-limit budget behind the same module.
func Mcsr(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(mcsrModuleName, module.KindOptIn)
	m.Command("elo").Everyone().Cooldown(mcsrCooldown).Aliases("mcsr", "ranked").
		Run(mcsrEloRun(d))
	m.Command("session").Everyone().Cooldown(mcsrCooldown).Aliases("mcsrsession").
		Run(mcsrSessionRun(d))
	m.Command("pace").Everyone().Cooldown(mcsrCooldown).Aliases("pacesession", "splits").
		Run(mcsrPaceRun(d))
	m.Command("nethers").Everyone().Cooldown(mcsrCooldown).Aliases("nph").
		Run(mcsrNethersRun(d))
	m.Command("lastfort").Everyone().Cooldown(mcsrCooldown).Aliases("lastpace", "previousfort").
		Run(mcsrLastFortRun(d))
	m.Command("lastmatch").Everyone().Cooldown(mcsrCooldown).Aliases("rankedmatch").
		Run(mcsrLastMatchRun(d))
	m.Command("record").Everyone().Cooldown(mcsrCooldown).Aliases("matchrecord").
		Run(mcsrRecordRun(d))
	m.Command("lb").Everyone().Cooldown(mcsrCooldown).Aliases("leaderboard", "rankedlb").
		Run(mcsrLbRun(d))
	m.Command("race").Everyone().Cooldown(mcsrCooldown).Aliases("weeklyrace").
		Run(mcsrRaceRun(d))

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
		go func() {
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
		}()
		return nil
	})

	return m.Build()
}

// mcsrEloRun answers !elo with the player's current standing. Template tokens:
// {player} {elo} {rank} {wins} {losses} {matches} {country}.
func mcsrEloRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.EloEnabled) || d.Gossip == nil {
			return nil
		}

		rest, season := parseMcsrSeason(args)
		account := resolveAccount(accountSources{Arg: rest, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		var reply gossiprpc.McsrUserReply
		req := gossiprpc.Request{Account: account, Season: season, IsPremium: c.Regress.IsPremium()}
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "mcsr", Endpoint: "user"}, req, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		tmpl := orDefault(cfg.EloMessage, defaultMcsrEloTemplate)
		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "player":
				return reply.Nickname, true
			case "elo":
				return mcsrElo(c.Locale, reply.Elo), true
			case "rank":
				return mcsrRank(reply.Rank), true
			case "wins":
				return strconv.Itoa(reply.Wins), true
			case "losses":
				return strconv.Itoa(reply.Loses), true
			case "matches":
				return strconv.Itoa(reply.Played), true
			case "country":
				return reply.Country, true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// mcsrSessionRun answers !session with the delta since the stream-start
// snapshot. Template tokens: {player} {elo} {elochange} {wins} {losses}
// {matches}. Without a baseline (module enabled mid-stream) gossip starts
// tracking now and the reply says so instead of faking a zero delta.
func mcsrSessionRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.SessionEnabled) || d.Gossip == nil {
			return nil
		}

		// !session is always the linked account, never a typed argument: the
		// baseline snapshot is stored per channel and keyed to the linked
		// account, so honoring an arbitrary player would clobber the streamer's
		// stream-start baseline. Per-player lookups go through !elo instead.
		account := resolveAccount(accountSources{Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		req := gossiprpc.Request{Account: account, ChannelID: strconv.FormatUint(c.BroadcasterID, 10), IsPremium: c.Regress.IsPremium()}
		var reply gossiprpc.McsrSessionReply
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "mcsr", Endpoint: "session"}, req, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		if !reply.HasSnapshot {
			emit(&module.Output{
				Type:          outgress.TypeChat,
				BroadcasterID: c.Env.BroadcasterUserID,
				Text:          reply.Nickname + ": " + fmt.Sprintf(i18n.T(c.Locale, "mcsr.session.started"), mcsrElo(c.Locale, reply.Elo)),
			})
			return nil
		}

		tmpl := orDefault(cfg.SessionMessage, defaultMcsrSessionTemplate)
		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "player":
				return reply.Nickname, true
			case "elo":
				return mcsrElo(c.Locale, reply.Elo), true
			case "elochange":
				return signed(reply.EloChange), true
			case "wins":
				return strconv.Itoa(reply.Wins), true
			case "losses":
				return strconv.Itoa(reply.Loses), true
			case "matches":
				return strconv.Itoa(reply.Played), true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// mcsrElo renders an elo value, naming the unrated sentinel.
func mcsrElo(locale string, elo int) string {
	if elo < 0 {
		return i18n.T(locale, "mcsr.unrated")
	}
	return strconv.Itoa(elo)
}

// mcsrRank renders a leaderboard rank, dashing the unranked sentinel.
func mcsrRank(rank int) string {
	if rank < 0 {
		return "—"
	}
	return strconv.Itoa(rank)
}

// mcsrPaceRun answers !pace with this session's PaceMan split averages,
// nether count and nethers-per-hour. Template tokens: {player} {nethers}
// {nether} {bastion} {fortress} {firstportal} {stronghold} {end} {finish}
// {nph}. No nethers tracked this window is a normal PaceMan answer (the
// player simply hasn't started a run), not an error, so it gets a plain
// i18n line instead of a template full of zeroes.
func mcsrPaceRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.PaceEnabled) || d.Gossip == nil {
			return nil
		}

		account := resolveAccount(accountSources{Arg: args, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		var reply gossiprpc.PacemanSessionReply
		req := gossiprpc.Request{Account: account, IsPremium: c.Regress.IsPremium()}
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "paceman", Endpoint: "session"}, req, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		if reply.Empty {
			emitPaceEmpty(c, emit, reply.Player, "mcsr.pace.empty")
			return nil
		}

		tmpl := orDefault(cfg.PaceMessage, defaultMcsrPaceTemplate)
		msg := module.ExpandString(tmpl, mcsrPaceTokens(reply))
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// mcsrPaceTokens resolves !pace's template tokens. Split across two switches
// (session-summary vs. per-split averages) rather than one ten-case block,
// so neither half is the thing that trips the complexity gate.
func mcsrPaceTokens(reply gossiprpc.PacemanSessionReply) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if v, ok := mcsrPaceSummaryToken(reply, key); ok {
			return v, true
		}
		return mcsrPaceSplitToken(reply, key)
	}
}

func mcsrPaceSummaryToken(reply gossiprpc.PacemanSessionReply, key string) (string, bool) {
	switch key {
	case "player":
		return reply.Player, true
	case "nethers":
		return strconv.Itoa(reply.NetherCount), true
	case "nether":
		return reply.Nether, true
	case "nph":
		return trimScore(reply.NPH), true
	}
	return "", false
}

func mcsrPaceSplitToken(reply gossiprpc.PacemanSessionReply, key string) (string, bool) {
	switch key {
	case "bastion":
		return reply.Bastion, true
	case "fortress":
		return reply.Fortress, true
	case "firststructure":
		return reply.FirstStructure, true
	case "secondstructure":
		return reply.SecondStructure, true
	case "firstportal":
		return reply.FirstPortal, true
	case "stronghold":
		return reply.Stronghold, true
	case "end":
		return reply.End, true
	case "finish":
		return reply.Finish, true
	default:
		return module.ParseDynamic(key)
	}
}

// mcsrNethersRun answers !nethers with just the session's nether-entrance
// count and pace. Template tokens: {player} {nethers} {nether} {nph}.
func mcsrNethersRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.NethersEnabled) || d.Gossip == nil {
			return nil
		}

		account := resolveAccount(accountSources{Arg: args, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		var reply gossiprpc.PacemanNethersReply
		req := gossiprpc.Request{Account: account, IsPremium: c.Regress.IsPremium()}
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "paceman", Endpoint: "nethers"}, req, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		if reply.Empty {
			emitPaceEmpty(c, emit, reply.Player, "mcsr.pace.empty")
			return nil
		}

		tmpl := orDefault(cfg.NethersMessage, defaultMcsrNethersTemplate)
		msg := module.ExpandString(tmpl, func(key string) (string, bool) {
			switch key {
			case "player":
				return reply.Player, true
			case "nethers":
				return strconv.Itoa(reply.Count), true
			case "nether":
				return reply.Avg, true
			case "nph":
				return trimScore(reply.NPH), true
			default:
				return module.ParseDynamic(key)
			}
		})
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// mcsrLastFortRun answers !lastfort with the most recent run that reached a
// second structure (bastion or fortress). Template tokens: {player} {nether}
// {bastion} {fortress} {firstportal} {stronghold} {ago}. An empty lookback
// window (no fortress pace recently) is a normal answer, not an error.
func mcsrLastFortRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.LastFortEnabled) || d.Gossip == nil {
			return nil
		}

		account := resolveAccount(accountSources{Arg: args, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		var reply gossiprpc.PacemanLastFortReply
		req := gossiprpc.Request{Account: account, IsPremium: c.Regress.IsPremium()}
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "paceman", Endpoint: "lastfort"}, req, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		if reply.Empty {
			emitPaceEmpty(c, emit, reply.Player, "mcsr.pace.nofort")
			return nil
		}

		tmpl := orDefault(cfg.LastFortMessage, defaultMcsrLastFortTemplate)
		msg := module.ExpandString(tmpl, mcsrLastFortTokens(reply))
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

func mcsrLastFortTokens(reply gossiprpc.PacemanLastFortReply) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if v, ok := mcsrLastFortSplitToken(reply, key); ok {
			return v, true
		}
		switch key {
		case "player":
			return reply.Player, true
		case "ago":
			return mcsrAge(reply.AgoSeconds), true
		default:
			return module.ParseDynamic(key)
		}
	}
}

// mcsrLastFortSplitToken renders the run's split tokens. It is separate from
// the reply's own fields so the token switch stays inside the complexity
// budget as splits are added.
func mcsrLastFortSplitToken(reply gossiprpc.PacemanLastFortReply, key string) (string, bool) {
	switch key {
	case "nether":
		return mcsrSplit(reply.Nether), true
	case "bastion":
		return mcsrSplit(reply.Bastion), true
	case "fortress":
		return mcsrSplit(reply.Fortress), true
	case "firstportal":
		return mcsrSplit(reply.FirstPortal), true
	case "stronghold":
		return mcsrSplit(reply.Stronghold), true
	case "end":
		return mcsrSplit(reply.End), true
	case "finish":
		return mcsrSplit(reply.Finish), true
	}
	return "", false
}

// emitPaceEmpty answers a pace command that found nothing to report: no
// nethers tracked this session, or no fortress pace in the lookback window.
// Both are normal PaceMan answers, not errors, so they get one plain chat
// line via i18n instead of a template with nothing to fill it.
func emitPaceEmpty(c *module.Context, emit module.Emit, player, key string) {
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          player + ": " + i18n.T(c.Locale, key),
	})
}

// mcsrSplit renders a lastfort split, dashing a run that never reached it
// (the provider answers "" for that case).
func mcsrSplit(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// mcsrAge renders a lastfort run's age as a short human duration: enough
// resolution to answer "how stale is this pace" without a full clock string.
func mcsrAge(seconds int64) string {
	switch {
	case seconds < 60:
		return "<1m"
	case seconds < 3600:
		return strconv.FormatInt(seconds/60, 10) + "m"
	case seconds < 86400:
		return strconv.FormatInt(seconds/3600, 10) + "h"
	default:
		return strconv.FormatInt(seconds/86400, 10) + "d"
	}
}

// parseMcsrSeason splits a trailing "season:<n>" token off a command's typed
// argument string. It is shared by !elo, !lastmatch, !record and !lb: all
// four forward Season on their gossip request, so the parsing lives once
// here instead of once per command. Returns the args with that token
// removed (so the remaining text still resolves cleanly as a player name)
// and the parsed season, or 0 when no valid token was present — gossip then
// omits Season and the provider defaults to "current season", identical to
// today's behavior when nobody types the token at all.
func parseMcsrSeason(args string) (rest string, season int) {
	fields := strings.Fields(args)
	kept := make([]string, 0, len(fields))
	for _, f := range fields {
		if season == 0 {
			if n, ok := mcsrSeasonValue(f); ok {
				season = n
				continue
			}
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " "), season
}

// mcsrSeasonPrefix is the token prefix parseMcsrSeason looks for.
const mcsrSeasonPrefix = "season:"

func mcsrSeasonValue(field string) (int, bool) {
	if !strings.HasPrefix(strings.ToLower(field), mcsrSeasonPrefix) {
		return 0, false
	}
	n, err := strconv.Atoi(field[len(mcsrSeasonPrefix):])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// mcsrLastMatchRun answers !lastmatch with the player's most recent match.
// Template tokens: {player} {opponent} {result} {time} {seed} {structure}
// {elochange} {ago}. No matches at all is a normal MCSR answer (a brand-new
// player), not an error, so it gets a plain i18n line instead of a template
// full of blanks.
func mcsrLastMatchRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.LastMatchEnabled) || d.Gossip == nil {
			return nil
		}

		rest, season := parseMcsrSeason(args)
		account := resolveAccount(accountSources{Arg: rest, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		var reply gossiprpc.McsrLastMatchReply
		req := gossiprpc.Request{Account: account, Season: season, IsPremium: c.Regress.IsPremium()}
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "mcsr", Endpoint: "last_match"}, req, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		if reply.Empty {
			emit(&module.Output{
				Type:          outgress.TypeChat,
				BroadcasterID: c.Env.BroadcasterUserID,
				Text:          reply.Player + ": " + i18n.T(c.Locale, "mcsr.lastmatch.empty"),
			})
			return nil
		}

		tmpl := orDefault(cfg.LastMatchMessage, defaultMcsrLastMatchTemplate)
		msg := module.ExpandString(tmpl, mcsrLastMatchTokens(c.Locale, reply))
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// mcsrLastMatchTokens is split across two switches (summary vs. per-match
// detail) so neither half is the thing that trips the complexity gate, same
// as mcsrPaceTokens above.
func mcsrLastMatchTokens(locale string, reply gossiprpc.McsrLastMatchReply) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if v, ok := mcsrLastMatchSummaryToken(locale, reply, key); ok {
			return v, true
		}
		return mcsrLastMatchDetailToken(reply, key)
	}
}

func mcsrLastMatchSummaryToken(locale string, reply gossiprpc.McsrLastMatchReply, key string) (string, bool) {
	switch key {
	case "player":
		return reply.Player, true
	case "opponent":
		return reply.Opponent, true
	case "result":
		return mcsrMatchResultText(locale, reply), true
	case "elochange":
		return signed(reply.EloChange), true
	}
	return "", false
}

func mcsrLastMatchDetailToken(reply gossiprpc.McsrLastMatchReply, key string) (string, bool) {
	switch key {
	case "time":
		return mcsrSplit(reply.Time), true
	case "seed":
		return mcsrSplit(reply.Seed), true
	case "structure":
		return mcsrSplit(reply.Structure), true
	case "ago":
		return mcsrAge(reply.AgoSeconds), true
	default:
		return module.ParseDynamic(key)
	}
}

// mcsrMatchResultText renders {result} so a forfeit or decay match never
// reads like an ordinary completed race: Result alone ("win"/"loss"/"draw")
// would claim a real finish happened when the match may never have reached
// one.
func mcsrMatchResultText(locale string, reply gossiprpc.McsrLastMatchReply) string {
	base := mcsrResultWord(locale, reply.Result)
	switch {
	case reply.Forfeited:
		return base + " " + i18n.T(locale, "mcsr.lastmatch.forfeit")
	case reply.Decayed:
		return base + " " + i18n.T(locale, "mcsr.lastmatch.decay")
	default:
		return base
	}
}

func mcsrResultWord(locale, result string) string {
	switch result {
	case "win":
		return i18n.T(locale, "mcsr.lastmatch.win")
	case "loss":
		return i18n.T(locale, "mcsr.lastmatch.loss")
	default:
		return i18n.T(locale, "mcsr.lastmatch.draw")
	}
}

// mcsrRecordRun answers !record with the head-to-head totals between two
// players. Template tokens: {playera} {playerb} {winsa} {winsb} {played}.
func mcsrRecordRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.RecordEnabled) || d.Gossip == nil {
			return nil
		}

		rest, season := parseMcsrSeason(args)
		accountA, accountB := mcsrRecordAccounts(rest, cfg, c)
		if accountA == "" || accountB == "" {
			emit(&module.Output{
				Type:          outgress.TypeChat,
				BroadcasterID: c.Env.BroadcasterUserID,
				Text:          i18n.T(c.Locale, "mcsr.record.usage"),
			})
			return nil
		}

		var reply gossiprpc.McsrRecordReply
		req := gossiprpc.Request{Account: accountA, AccountB: accountB, Season: season, IsPremium: c.Regress.IsPremium()}
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "mcsr", Endpoint: "versus"}, req, &reply); err != nil {
			if chatReplyError(c, emit, accountA, err) {
				return nil
			}
			return err
		}

		tmpl := orDefault(cfg.RecordMessage, defaultMcsrRecordTemplate)
		msg := module.ExpandString(tmpl, mcsrRecordTokens(reply))
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// mcsrRecordAccounts resolves !record's two sides. Two typed usernames
// compare those two directly; one typed username compares it against the
// module's linked account, the "how do I stack up against them" shorthand
// the command promises. Zero typed usernames has nothing to compare, so both
// come back empty and the caller sends the usage line instead of a call.
func mcsrRecordAccounts(args string, cfg mcsrConfig, c *module.Context) (a, b string) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return "", ""
	}
	first := strings.TrimPrefix(fields[0], "@")
	if len(fields) == 1 {
		self := resolveAccount(accountSources{Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		return self, first
	}
	return first, strings.TrimPrefix(fields[1], "@")
}

func mcsrRecordTokens(reply gossiprpc.McsrRecordReply) func(string) (string, bool) {
	return func(key string) (string, bool) {
		switch key {
		case "playera":
			return reply.PlayerA, true
		case "playerb":
			return reply.PlayerB, true
		case "winsa":
			return strconv.Itoa(reply.WinsA), true
		case "winsb":
			return strconv.Itoa(reply.WinsB), true
		case "played":
			return strconv.Itoa(reply.Played), true
		default:
			return module.ParseDynamic(key)
		}
	}
}

// mcsrLbRun answers !lb with the top of one leaderboard. Sub-argument picks
// the board (default elo; "phase" for phase points, add "predicted" for the
// current season's projected points; "record" for season-best times); an
// optional "country:<cc>" token filters every board but record (the
// provider drops it there rather than erroring, per the upstream's own
// limitation). Template tokens: {board} {list}; {list} is the whole "#1
// Name 2010 · #2 Name2 1990 · ..." line since chat gets one line no matter
// how the broadcaster's template wraps it.
func mcsrLbRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.LbEnabled) || d.Gossip == nil {
			return nil
		}

		rest, season := parseMcsrSeason(args)
		board, country, predicted := parseMcsrBoardArgs(rest)

		var reply gossiprpc.McsrLeaderboardReply
		req := gossiprpc.Request{Board: board, Country: country, Predicted: predicted, Season: season, IsPremium: c.Regress.IsPremium()}
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "mcsr", Endpoint: "leaderboard"}, req, &reply); err != nil {
			if chatReplyError(c, emit, mcsrBoardLabel(board), err) {
				return nil
			}
			return err
		}

		if reply.Empty {
			emit(&module.Output{
				Type:          outgress.TypeChat,
				BroadcasterID: c.Env.BroadcasterUserID,
				Text:          mcsrBoardLabel(reply.Board) + ": " + i18n.T(c.Locale, "mcsr.leaderboard.empty"),
			})
			return nil
		}

		tmpl := orDefault(cfg.LbMessage, defaultMcsrLbTemplate)
		msg := module.ExpandString(tmpl, mcsrLbTokens(reply))
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

// parseMcsrBoardArgs reads !lb's board word ("phase"/"record", default
// elo), the "predicted" flag and an optional "country:<cc>" token out of the
// (season-stripped) argument string. Tokens are unordered flags rather than
// positional args so "!lb country:us phase" and "!lb phase country:us" both
// work.
func parseMcsrBoardArgs(args string) (board, country string, predicted bool) {
	for _, f := range strings.Fields(args) {
		lf := strings.ToLower(f)
		switch {
		case lf == "phase":
			board = "phase"
		case lf == "record":
			board = "record"
		case lf == "predicted":
			predicted = true
		case strings.HasPrefix(lf, "country:"):
			country = strings.TrimPrefix(lf, "country:")
		}
	}
	return board, country, predicted
}

func mcsrBoardLabel(board string) string {
	switch board {
	case "phase":
		return "Phase"
	case "record":
		return "Record"
	default:
		return "Elo"
	}
}

func mcsrLbTokens(reply gossiprpc.McsrLeaderboardReply) func(string) (string, bool) {
	return func(key string) (string, bool) {
		switch key {
		case "board":
			return mcsrBoardLabel(reply.Board), true
		case "list":
			return mcsrFormatLeaderboard(reply.Entries), true
		default:
			return module.ParseDynamic(key)
		}
	}
}

// mcsrFormatLeaderboard joins the reply's top entries into the one chat line
// !lb promises: "#1 Name 2010 · #2 Name2 1990 · ...".
func mcsrFormatLeaderboard(entries []gossiprpc.McsrLeaderboardEntry) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, "#"+strconv.Itoa(e.Rank)+" "+e.Name+" "+e.Value)
	}
	return strings.Join(parts, " · ")
}

// mcsrRaceRun answers !race with the weekly-race seed's #1 holder and the
// queried player's own time and placement. Template tokens: {leader}
// {leadertime} {player} {time} {rank}. No season token: the upstream does
// not accept one on this endpoint. A player with no time yet this week
// still gets the leader info plus a plain i18n line, not silence or an
// error.
func mcsrRaceRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		var cfg mcsrConfig
		_ = c.Decode(&cfg)
		if !alertOn(cfg.RaceEnabled) || d.Gossip == nil {
			return nil
		}

		account := resolveAccount(accountSources{Arg: args, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		var reply gossiprpc.McsrWeeklyRaceReply
		req := gossiprpc.Request{Account: account, IsPremium: c.Regress.IsPremium()}
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "mcsr", Endpoint: "weekly_race"}, req, &reply); err != nil {
			if chatReplyError(c, emit, account, err) {
				return nil
			}
			return err
		}

		if reply.Empty {
			emit(&module.Output{
				Type:          outgress.TypeChat,
				BroadcasterID: c.Env.BroadcasterUserID,
				Text:          i18n.T(c.Locale, "mcsr.race.empty"),
			})
			return nil
		}
		if !reply.HasPlayer {
			emit(&module.Output{
				Type:          outgress.TypeChat,
				BroadcasterID: c.Env.BroadcasterUserID,
				Text:          mcsrRaceLeaderText(reply) + " · " + reply.Player + ": " + i18n.T(c.Locale, "mcsr.race.noplayer"),
			})
			return nil
		}

		tmpl := orDefault(cfg.RaceMessage, defaultMcsrRaceTemplate)
		msg := module.ExpandString(tmpl, mcsrRaceTokens(reply))
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: msg})
		return nil
	}
}

func mcsrRaceLeaderText(reply gossiprpc.McsrWeeklyRaceReply) string {
	return "#1 " + reply.LeaderName + " (" + reply.LeaderTime + ")"
}

func mcsrRaceTokens(reply gossiprpc.McsrWeeklyRaceReply) func(string) (string, bool) {
	return func(key string) (string, bool) {
		switch key {
		case "leader":
			return reply.LeaderName, true
		case "leadertime":
			return reply.LeaderTime, true
		case "player":
			return reply.Player, true
		case "time":
			return reply.PlayerTime, true
		case "rank":
			return strconv.Itoa(reply.PlayerRank), true
		default:
			return module.ParseDynamic(key)
		}
	}
}
