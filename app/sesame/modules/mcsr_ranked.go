// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// This file holds the MCSR Ranked commands: !elo, !session, !lastmatch,
// !record, !lb, !race and !pb. They share the mcsrHandler shape defined in
// mcsr.go; each Run function supplies only the toggle, the gossip endpoint,
// the request shape and how to render a successful reply. Template-token
// resolvers build a map and hand it to mcsrTokenLookup (in mcsr.go) rather
// than each rolling its own switch/fallback dispatch.

// mcsrSeasonCommands builds the season-scoped command family's wiring from
// one table: !elo and !lastmatch both accept a trailing "season:<n>" token
// (mcsrSeasonRunFunc) and share mcsrSeasonCommand's shape, differing only in
// these fields.
func mcsrSeasonCommands(d engine.Deps) []mcsrCommandReg {
	return []mcsrCommandReg{
		{
			name:    "elo",
			aliases: []string{"mcsr", "ranked"},
			run: mcsrSeasonRunFunc(d, mcsrSeasonSpec[gossiprpc.McsrUserReply]{
				enabled:  func(cfg mcsrConfig) string { return cfg.EloEnabled },
				endpoint: "user",
				message:  func(cfg mcsrConfig) string { return cfg.EloMessage },
				template: defaultMcsrEloTemplate,
				tokens:   mcsrEloTokens,
			}),
		},
		{
			name:    "lastmatch",
			aliases: []string{"rankedmatch"},
			run: mcsrSeasonRunFunc(d, mcsrSeasonSpec[gossiprpc.McsrLastMatchReply]{
				enabled:  func(cfg mcsrConfig) string { return cfg.LastMatchEnabled },
				endpoint: "last_match",
				isEmpty: func(r gossiprpc.McsrLastMatchReply) (string, string, bool) {
					return r.Player, "mcsr.lastmatch.empty", r.Empty
				},
				message:  func(cfg mcsrConfig) string { return cfg.LastMatchMessage },
				template: defaultMcsrLastMatchTemplate,
				tokens:   mcsrLastMatchTokens,
			}),
		},
	}
}

// mcsrEloTokens resolves !elo's template tokens: {player} {elo} {rank}
// {wins} {losses} {draws} {matches} {country}.
func mcsrEloTokens(c *module.Context, reply gossiprpc.McsrUserReply) func(string) (string, bool) {
	tokens := mcsrMergeTokens(
		mcsrPlayerEloTokens(c, reply.Nickname, reply.Elo),
		mcsrWinLossTokens(reply.Wins, reply.Loses, reply.Played),
		map[string]string{
			"rank":    mcsrRank(reply.Rank),
			"country": reply.Country,
		},
	)
	return func(key string) (string, bool) { return mcsrTokenLookup(tokens, key) }
}

// mcsrSessionRun answers !session with the delta since the stream-start
// snapshot. Template tokens: {player} {elo} {elochange} {wins} {losses}
// {draws} {matches}. Without a baseline (module enabled mid-stream) gossip starts
// tracking now and the reply says so instead of faking a zero delta.
//
// !session is always the linked account, never a typed argument: the
// baseline snapshot is stored per channel and keyed to the linked account,
// so honoring an arbitrary player would clobber the streamer's stream-start
// baseline. Per-player lookups go through !elo instead — the wrapper below
// discards whatever the viewer typed before handing off to mcsrHandler.
func mcsrSessionRun(d engine.Deps) module.RunFunc {
	h := mcsrHandler[gossiprpc.McsrSessionReply]{
		d:       d,
		enabled: func(cfg mcsrConfig) string { return cfg.SessionEnabled },
		route:   engine.GossipRoute{Provider: "mcsr", Endpoint: "session"},
		request: func(c *module.Context, account string, cfg mcsrConfig) gossiprpc.Request {
			return gossiprpc.Request{Account: account, ChannelID: strconv.FormatUint(c.BroadcasterID, 10), IsPremium: c.Regress.IsPremium()}
		},
		reply: func(c *module.Context, cfg mcsrConfig, reply gossiprpc.McsrSessionReply) string {
			if !reply.HasSnapshot {
				return reply.Nickname + ": " + fmt.Sprintf(i18n.T(c.Locale, "mcsr.session.started"), mcsrElo(c, reply.Elo))
			}
			tmpl := mcsrSessionTemplate(cfg.SessionMessage)
			return module.ExpandString(tmpl, mcsrSessionTokens(c, reply))
		},
	}
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		return h.run(ctx, c, "", emit)
	}
}

// mcsrSessionTemplate picks the template !session renders with: the stored
// message, the current default when nothing is stored, and the current default
// again when the stored message is byte-identical to the pre-{draws} default
// (see legacyMcsrSessionTemplate for why that case exists and why an edited
// template is never touched).
func mcsrSessionTemplate(stored string) string {
	if strings.TrimSpace(stored) == legacyMcsrSessionTemplate {
		return defaultMcsrSessionTemplate
	}
	return orDefault(stored, defaultMcsrSessionTemplate)
}

// mcsrSessionTokens resolves !session's template tokens: {player} {elo}
// {elochange} {wins} {losses} {draws} {matches}.
func mcsrSessionTokens(c *module.Context, reply gossiprpc.McsrSessionReply) func(string) (string, bool) {
	tokens := mcsrMergeTokens(
		mcsrPlayerEloTokens(c, reply.Nickname, reply.Elo),
		mcsrWinLossTokens(reply.Wins, reply.Loses, reply.Played),
		map[string]string{"elochange": signed(reply.EloChange)},
	)
	return func(key string) (string, bool) { return mcsrTokenLookup(tokens, key) }
}

// parseMcsrSeason-stripped args also drive !lastmatch, !record and !lb below.

// !lastmatch answers with the player's most recent match. Template tokens:
// {player} {opponent} {result} {time} {seed} {structure} {elochange} {ago}.
// No matches at all is a normal MCSR answer (a brand-new player), not an
// error, so mcsrSeasonCommands' isEmpty sends a plain i18n line instead of a
// template full of blanks. See mcsrSeasonCommands above for its wiring.

// mcsrLastMatchTokens resolves !lastmatch's template tokens: {player}
// {opponent} {result} {elochange} {time} {seed} {structure} {ago}.
func mcsrLastMatchTokens(c *module.Context, reply gossiprpc.McsrLastMatchReply) func(string) (string, bool) {
	tokens := map[string]string{
		"player":    reply.Player,
		"opponent":  reply.Opponent,
		"result":    mcsrMatchResultText(c, reply),
		"elochange": signed(reply.EloChange),
		"time":      mcsrSplit(reply.Time),
		"seed":      mcsrSplit(reply.Seed),
		"structure": mcsrSplit(reply.Structure),
		"ago":       mcsrAge(reply.AgoSeconds),
	}
	return func(key string) (string, bool) { return mcsrTokenLookup(tokens, key) }
}

// mcsrMatchResultText renders {result} so a forfeit or decay match never
// reads like an ordinary completed race: Result alone ("win"/"loss"/"draw")
// would claim a real finish happened when the match may never have reached
// one.
func mcsrMatchResultText(c *module.Context, reply gossiprpc.McsrLastMatchReply) string {
	base := mcsrResultWord(c, reply.Result)
	switch {
	case reply.Forfeited:
		return base + " " + i18n.T(c.Locale, "mcsr.lastmatch.forfeit")
	case reply.Decayed:
		return base + " " + i18n.T(c.Locale, "mcsr.lastmatch.decay")
	default:
		return base
	}
}

func mcsrResultWord(c *module.Context, result string) string {
	switch result {
	case "win":
		return i18n.T(c.Locale, "mcsr.lastmatch.win")
	case "loss":
		return i18n.T(c.Locale, "mcsr.lastmatch.loss")
	default:
		return i18n.T(c.Locale, "mcsr.lastmatch.draw")
	}
}

// mcsrRecordRun answers !record with the head-to-head totals between two
// players. Template tokens: {playera} {playerb} {winsa} {winsb} {played}.
// It resolves two accounts instead of one and has no "empty" reply shape, so
// it does not fit mcsrHandler's single-account contract and stays hand-rolled.
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
			mcsrEmit(c, emit, i18n.T(c.Locale, "mcsr.record.usage"))
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
		mcsrEmit(c, emit, module.ExpandString(tmpl, mcsrRecordTokens(reply)))
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

// mcsrRecordTokens resolves !record's template tokens: {playera} {playerb}
// {winsa} {winsb} {played}.
func mcsrRecordTokens(reply gossiprpc.McsrRecordReply) func(string) (string, bool) {
	tokens := map[string]string{
		"playera": reply.PlayerA,
		"playerb": reply.PlayerB,
		"winsa":   strconv.Itoa(reply.WinsA),
		"winsb":   strconv.Itoa(reply.WinsB),
		"played":  strconv.Itoa(reply.Played),
	}
	return func(key string) (string, bool) { return mcsrTokenLookup(tokens, key) }
}

// mcsrLbRun answers !lb with the top of one leaderboard. Sub-argument picks
// the board (default elo; "phase" for phase points, add "predicted" for the
// current season's projected points; "record" for season-best times); an
// optional "country:<cc>" token filters every board but record (the
// provider drops it there rather than erroring, per the upstream's own
// limitation). Template tokens: {board} {list}; {list} is the whole "#1
// Name 2010 · #2 Name2 1990 · ..." line since chat gets one line no matter
// how the broadcaster's template wraps it.
//
// !lb selects a board instead of resolving a player account, so it does not
// fit mcsrHandler's single-account contract and stays hand-rolled.
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
			mcsrEmit(c, emit, mcsrBoardLabel(reply.Board)+": "+i18n.T(c.Locale, "mcsr.leaderboard.empty"))
			return nil
		}

		tmpl := orDefault(cfg.LbMessage, defaultMcsrLbTemplate)
		mcsrEmit(c, emit, module.ExpandString(tmpl, mcsrLbTokens(reply)))
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

// mcsrLbTokens resolves !lb's template tokens: {board} {list}.
func mcsrLbTokens(reply gossiprpc.McsrLeaderboardReply) func(string) (string, bool) {
	tokens := map[string]string{
		"board": mcsrBoardLabel(reply.Board),
		"list":  mcsrFormatLeaderboard(reply.Entries),
	}
	return func(key string) (string, bool) { return mcsrTokenLookup(tokens, key) }
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
	h := mcsrHandler[gossiprpc.McsrWeeklyRaceReply]{
		d:       d,
		enabled: func(cfg mcsrConfig) string { return cfg.RaceEnabled },
		route:   engine.GossipRoute{Provider: "mcsr", Endpoint: "weekly_race"},
		request: mcsrSimpleRequest,
		reply: func(c *module.Context, cfg mcsrConfig, reply gossiprpc.McsrWeeklyRaceReply) string {
			if reply.Empty {
				return i18n.T(c.Locale, "mcsr.race.empty")
			}
			if !reply.HasPlayer {
				return mcsrRaceLeaderText(reply) + " · " + reply.Player + ": " + i18n.T(c.Locale, "mcsr.race.noplayer")
			}
			tmpl := orDefault(cfg.RaceMessage, defaultMcsrRaceTemplate)
			return module.ExpandString(tmpl, mcsrRaceTokens(reply))
		},
	}
	return h.run
}

func mcsrRaceLeaderText(reply gossiprpc.McsrWeeklyRaceReply) string {
	return "#1 " + reply.LeaderName + " (" + reply.LeaderTime + ")"
}

// mcsrRaceTokens resolves !race's template tokens: {leader} {leadertime}
// {player} {time} {rank}.
func mcsrRaceTokens(reply gossiprpc.McsrWeeklyRaceReply) func(string) (string, bool) {
	tokens := map[string]string{
		"leader":     reply.LeaderName,
		"leadertime": reply.LeaderTime,
		"player":     reply.Player,
		"time":       reply.PlayerTime,
		"rank":       strconv.Itoa(reply.PlayerRank),
	}
	return func(key string) (string, bool) { return mcsrTokenLookup(tokens, key) }
}

// --- !pb (PaceMan personal best / MCSR Ranked season best) -----------------

// mcsrPbWindows is the set of window keywords !pb recognizes as its first
// argument. Anything else (including nothing) falls through to the bare-name
// form: parseMcsrPbArgs then treats the whole argument string as a player
// name and mcsrPbRun defaults the window to all-time.
var mcsrPbWindows = map[string]bool{
	"daily":   true,
	"weekly":  true,
	"monthly": true,
	"ranked":  true,
}

// parseMcsrPbArgs splits !pb's optional leading window keyword off the rest
// of the argument string, mirroring parseMcsrSeason's "peel a recognized
// token, leave everything else for account resolution" shape. window is ""
// when no recognized keyword was typed (the bare "!pb" and "!pb <player>"
// forms), which mcsrPbRun then treats as all-time.
func parseMcsrPbArgs(args string) (window, rest string) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return "", ""
	}
	first := strings.ToLower(fields[0])
	if mcsrPbWindows[first] {
		return first, strings.Join(fields[1:], " ")
	}
	return "", args
}

// mcsrPbRun answers !pb. The window keyword picks one of two independent
// upstreams (paceman's precomputed pbs for daily/weekly/monthly/all-time, or
// mcsr's own season-best for ranked), so each branch below builds its own
// mcsrHandler instance rather than forcing one reply type across both.
//
// The ranked branch reads BestTimeMS off the same "user" endpoint !elo
// already calls — it is fetched there but was unused until !pb ranked needed
// it, so this adds no new upstream call. A 0 BestTimeMS covers both "rated
// but no ranked completion yet" and "unrated" (the upstream never populates
// a season best for either) — both read as the same "no personal best" line.
func mcsrPbRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		window, rest := parseMcsrPbArgs(args)

		if window == "ranked" {
			h := mcsrHandler[gossiprpc.McsrUserReply]{
				d:       d,
				enabled: func(cfg mcsrConfig) string { return cfg.PbEnabled },
				route:   engine.GossipRoute{Provider: "mcsr", Endpoint: "user"},
				request: mcsrSimpleRequest,
				reply: func(c *module.Context, cfg mcsrConfig, reply gossiprpc.McsrUserReply) string {
					if reply.BestTimeMS <= 0 {
						return mcsrPbEmptyText(c, reply.Nickname, "ranked")
					}
					tmpl := orDefault(cfg.PbMessage, defaultMcsrPbTemplate)
					view := mcsrPbView{Player: reply.Nickname, Time: mcsrMsToClock(reply.BestTimeMS), WindowLabel: mcsrPbWindowLabel(c, "ranked")}
					return module.ExpandString(tmpl, mcsrPbTokens(view))
				},
			}
			return h.run(ctx, c, rest, emit)
		}

		h := mcsrHandler[gossiprpc.PacemanPersonalBestReply]{
			d:       d,
			enabled: func(cfg mcsrConfig) string { return cfg.PbEnabled },
			route:   engine.GossipRoute{Provider: "paceman", Endpoint: "personal_best"},
			request: func(c *module.Context, account string, cfg mcsrConfig) gossiprpc.Request {
				return gossiprpc.Request{Account: account, TimeWindow: window, IsPremium: c.Regress.IsPremium()}
			},
			reply: func(c *module.Context, cfg mcsrConfig, reply gossiprpc.PacemanPersonalBestReply) string {
				if reply.Empty {
					return mcsrPbEmptyText(c, reply.Player, reply.Window)
				}
				tmpl := orDefault(cfg.PbMessage, defaultMcsrPbTemplate)
				view := mcsrPbView{Player: reply.Player, Time: reply.Time, WindowLabel: mcsrPbWindowLabel(c, reply.Window)}
				return module.ExpandString(tmpl, mcsrPbTokens(view))
			},
		}
		return h.run(ctx, c, rest, emit)
	}
}

// mcsrPbView bundles !pb's three rendered fields so mcsrPbTokens takes one
// named value instead of three interchangeable strings (String Heavy
// Function Arguments) — both !pb branches above build one from their own
// reply shape before handing it to the shared token resolver.
type mcsrPbView struct {
	Player      string
	Time        string
	WindowLabel string
}

// mcsrPbTokens resolves !pb's template tokens: {player} {time} {window}.
func mcsrPbTokens(v mcsrPbView) func(string) (string, bool) {
	tokens := map[string]string{
		"player": v.Player,
		"time":   v.Time,
		"window": v.WindowLabel,
	}
	return func(key string) (string, bool) { return mcsrTokenLookup(tokens, key) }
}

// mcsrPbEmptyText answers a !pb lookup that found no personal best in the
// requested window: a normal PaceMan/MCSR answer (the player just hasn't set
// one there yet), not an error, so it renders one plain translated line
// instead of a template with a fake zero time.
func mcsrPbEmptyText(c *module.Context, player, window string) string {
	return player + ": " + fmt.Sprintf(i18n.T(c.Locale, "mcsr.pb.empty"), mcsrPbWindowLabel(c, window))
}

// mcsrPbWindowLabel translates a normalized window ("daily", "weekly",
// "monthly", "all-time" or "ranked") into the {window} token's display word,
// localized so both the successful-reply token and the empty-reply sentence
// it is interpolated into read as one language.
func mcsrPbWindowLabel(c *module.Context, window string) string {
	switch window {
	case "daily":
		return i18n.T(c.Locale, "mcsr.pb.window.daily")
	case "weekly":
		return i18n.T(c.Locale, "mcsr.pb.window.weekly")
	case "monthly":
		return i18n.T(c.Locale, "mcsr.pb.window.monthly")
	case "ranked":
		return i18n.T(c.Locale, "mcsr.pb.window.ranked")
	default:
		return i18n.T(c.Locale, "mcsr.pb.window.alltime")
	}
}

// mcsrMsToClock renders a completion time in milliseconds the way the mcsr
// and paceman providers both do (minutes:seconds.milliseconds), for the one
// caller here that reads a raw ms value straight from a reply (BestTimeMS)
// instead of a pre-formatted Time string.
func mcsrMsToClock(ms int64) string {
	if ms <= 0 {
		return ""
	}
	minutes := ms / 60000
	seconds := (ms % 60000) / 1000
	millis := ms % 1000
	return fmt.Sprintf("%d:%02d.%03d", minutes, seconds, millis)
}
