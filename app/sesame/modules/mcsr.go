// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"strconv"
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

		account := resolveAccount(accountSources{Arg: args, Linked: cfg.Account, BroadcasterLogin: c.Env.BroadcasterUserLogin})
		var reply gossiprpc.McsrUserReply
		if err := d.Gossip.Call(ctx, engine.GossipRoute{Provider: "mcsr", Endpoint: "user"}, gossiprpc.Request{Account: account, IsPremium: c.Regress.IsPremium()}, &reply); err != nil {
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
