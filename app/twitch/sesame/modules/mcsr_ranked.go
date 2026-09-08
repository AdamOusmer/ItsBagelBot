// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// This file holds the MCSR Ranked commands: !elo, !session, !lastmatch,
// !record, !lb, !race and !pb. Each is one binding of the shared statsHandler
// (external.go) — which toggle gates it, which endpoint answers it, who the
// lookup is about, what the reply reads like — with no mcsr-specific layer in
// between. The ones whose chat line is a plain template ride externalCommand
// on top of it; the ones whose tokens are language-dependent (an "unrated"
// elo, a translated win/loss/draw) render through a module.StringPalette
// built against the channel's locale.

// mcsrEloRun answers !elo with the player's current rating and season record.
// Template tokens: {player} {elo} {rank} {wins} {losses} {draws} {matches}
// {country}. A trailing "season:<n>" looks at a past season instead.
func mcsrEloRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.McsrUserReply
	h := mcsrCommand(d, mcsrRoute("user"), func(cfg mcsrConfig) string { return cfg.EloEnabled },
		func(call statsCall[mcsrConfig], r *reply) string {
			return mcsrEloPalette(call.Ctx, r).Expand(orDefault(call.Cfg.EloMessage, defaultMcsrEloTemplate))
		})
	return mcsrSeasonRun(h, mcsrAccountSeason)
}

// mcsrEloPalette resolves !elo's template tokens: {player} {elo} {rank}
// {wins} {losses} {draws} {matches} {country}.
func mcsrEloPalette(c *module.Context, r *gossiprpc.McsrUserReply) module.StringPalette {
	return mcsrPlayerElo(c, r.Nickname, r.Elo).Merge(
		mcsrWinLoss(r.Wins, r.Loses, r.Played),
		module.StringPalette{
			"rank":    mcsrRank(r.Rank),
			"country": r.Country,
		},
	)
}

// mcsrSessionRun answers !session with the delta since the stream-start
// snapshot. Template tokens: {player} {elo} {elochange} {wins} {losses}
// {draws} {matches}. Without a baseline (module enabled mid-stream) gossip
// starts tracking now and the reply says so instead of faking a zero delta.
//
// !session is always the linked account, never a typed argument (ignoreArgs):
// the baseline snapshot is stored per channel and keyed to the linked account,
// so honoring an arbitrary player would answer about somebody else's numbers
// against the streamer's snapshot. Per-player lookups go through !elo.
func mcsrSessionRun(d engine.Deps) module.RunFunc {
	h := mcsrCommand(d, mcsrRoute("session"), func(cfg mcsrConfig) string { return cfg.SessionEnabled }, mcsrSessionText)
	// The baseline gossip diffs against is filed per channel, so this one
	// request carries the channel id alongside the account.
	h.request = func(call statsCall[mcsrConfig], subject statsSubject) gossiprpc.Request {
		return gossiprpc.Request{
			Account:   subject.Account,
			ChannelID: strconv.FormatUint(call.Ctx.BroadcasterID, 10),
			IsPremium: call.Ctx.Regress.IsPremium(),
		}
	}
	return ignoreArgs(h.run)
}

// mcsrSessionText renders !session's chat line: the delta template when a
// baseline exists, otherwise the "tracking starts now" note carrying the elo
// the baseline was just taken at.
func mcsrSessionText(call statsCall[mcsrConfig], r *gossiprpc.McsrSessionReply) string {
	if !r.HasSnapshot {
		started := i18n.T(call.Ctx.Locale, "mcsr.session.started")
		return r.Nickname + ": " + fmt.Sprintf(started, mcsrElo(call.Ctx, r.Elo))
	}
	return mcsrSessionPalette(call.Ctx, r).Expand(mcsrSessionTemplate(call.Cfg.SessionMessage))
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

// mcsrSessionPalette resolves !session's template tokens: {player} {elo}
// {elochange} {wins} {losses} {draws} {matches}.
func mcsrSessionPalette(c *module.Context, r *gossiprpc.McsrSessionReply) module.StringPalette {
	return mcsrPlayerElo(c, r.Nickname, r.Elo).Merge(
		mcsrWinLoss(r.Wins, r.Loses, r.Played),
		module.StringPalette{"elochange": signed(r.EloChange)},
	)
}

// mcsrLastMatchRun answers !lastmatch with the player's most recent match.
// Template tokens: {player} {opponent} {result} {elochange} {time} {seed}
// {structure} {ago}. A trailing "season:<n>" looks at a past season instead.
// No matches at all is a normal MCSR answer (a brand-new player), not an
// error, so it chats a plain translated line rather than a template full of
// blanks.
func mcsrLastMatchRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.McsrLastMatchReply
	h := mcsrCommand(d, mcsrRoute("last_match"), func(cfg mcsrConfig) string { return cfg.LastMatchEnabled },
		func(call statsCall[mcsrConfig], r *reply) string {
			if r.Empty {
				return mcsrEmptyText(call.Ctx, r.Player, "mcsr.lastmatch.empty")
			}
			return mcsrLastMatchPalette(call.Ctx, r).Expand(orDefault(call.Cfg.LastMatchMessage, defaultMcsrLastMatchTemplate))
		})
	return mcsrSeasonRun(h, mcsrAccountSeason)
}

// mcsrLastMatchPalette resolves !lastmatch's template tokens: {player}
// {opponent} {result} {elochange} {time} {seed} {structure} {ago}.
func mcsrLastMatchPalette(c *module.Context, r *gossiprpc.McsrLastMatchReply) module.StringPalette {
	return module.StringPalette{
		"player":    r.Player,
		"opponent":  r.Opponent,
		"result":    mcsrMatchResultText(c, r),
		"elochange": signed(r.EloChange),
		"time":      mcsrSplit(r.Time),
		"seed":      mcsrSplit(r.Seed),
		"structure": mcsrSplit(r.Structure),
		"ago":       mcsrAge(r.AgoSeconds),
	}
}

// mcsrMatchResultText renders {result} so a forfeit or decay match never
// reads like an ordinary completed race: Result alone ("win"/"loss"/"draw")
// would claim a real finish happened when the match may never have reached
// one.
func mcsrMatchResultText(c *module.Context, r *gossiprpc.McsrLastMatchReply) string {
	base := mcsrResultWord(c, r.Result)
	switch {
	case r.Forfeited:
		return base + " " + i18n.T(c.Locale, "mcsr.lastmatch.forfeit")
	case r.Decayed:
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
//
// It rides the same skeleton as every other command: the strategies are handed
// the whole call, so resolving two accounts instead of one is what the target
// returns and what the request forwards. (An earlier note here claimed a
// two-account lookup did not fit; it did not fit the mcsr-local binding layer
// that used to sit in between, which passed the hooks a single resolved
// account and nothing else.) Arguments naming nobody to compare against are
// refused with the usage line, after the module's own toggle is checked.
func mcsrRecordRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.McsrRecordReply
	h := mcsrCommand(d, mcsrRoute("versus"), func(cfg mcsrConfig) string { return cfg.RecordEnabled },
		func(call statsCall[mcsrConfig], r *reply) string {
			return mcsrRecordTokens.Expand(orDefault(call.Cfg.RecordMessage, defaultMcsrRecordTemplate), r)
		})
	// Two players, not one: the pair resolves together (and refuses together)
	// in the target, and the request forwards both sides.
	h.target = mcsrRecordSubject
	return mcsrSeasonRun(h, mcsrVersusSeason)
}

// mcsrVersusSeason is !record's request: both sides of the comparison and that
// call's season.
func mcsrVersusSeason(season int) mcsrRequest {
	return func(call statsCall[mcsrConfig], subject statsSubject) gossiprpc.Request {
		return gossiprpc.Request{
			Account:   subject.Account,
			AccountB:  subject.AccountB,
			Season:    season,
			IsPremium: call.Ctx.Regress.IsPremium(),
		}
	}
}

// mcsrRecordSubject resolves !record's two sides, or refuses the call.
func mcsrRecordSubject(call statsCall[mcsrConfig]) statsSubject {
	a, b, displayA := mcsrRecordAccounts(call.Args, call.Cfg, call.Ctx)
	if a == "" || b == "" {
		return statsSubject{Refusal: i18n.T(call.Ctx.Locale, "mcsr.record.usage")}
	}
	return statsSubject{Account: a, AccountB: b, Display: displayA}
}

// mcsrRecordAccounts resolves !record's two sides. Two typed usernames
// compare those two directly; one typed username compares it against the
// module's linked account, the "how do I stack up against them" shorthand
// the command promises. Zero typed usernames has nothing to compare, so both
// come back empty and the caller refuses with the usage line instead of a
// call. Linked-only keeps the shorthand and drops the two-name form: a
// head-to-head needs an opponent by definition, so the broadcaster stays side
// A and the first typed name is always who they are compared against.
// displayA is side A's chat name for error lines: the typed name, or the
// linked username when side A is the stored uuid.
func mcsrRecordAccounts(args string, cfg mcsrConfig, c *module.Context) (a, b, displayA string) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return "", "", ""
	}
	first := strings.TrimPrefix(fields[0], "@")
	if len(fields) == 1 || explicitOn(cfg.LinkedOnly) {
		self, selfDisplay := resolveLinked(c, accountSources{
			Linked: cfg.Account, LinkedUUID: cfg.AccountUUID, PreferUUID: true,
		})
		return self, first, selfDisplay
	}
	return first, strings.TrimPrefix(fields[1], "@"), first
}

// mcsrRecordTokens resolves !record's template tokens: {playera} {playerb}
// {winsa} {winsb} {played}.
var mcsrRecordTokens = module.TokenExpander[gossiprpc.McsrRecordReply]{
	"playera": func(r *gossiprpc.McsrRecordReply) string { return r.PlayerA },
	"playerb": func(r *gossiprpc.McsrRecordReply) string { return r.PlayerB },
	"winsa":   func(r *gossiprpc.McsrRecordReply) string { return strconv.Itoa(r.WinsA) },
	"winsb":   func(r *gossiprpc.McsrRecordReply) string { return strconv.Itoa(r.WinsB) },
	"played":  func(r *gossiprpc.McsrRecordReply) string { return strconv.Itoa(r.Played) },
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
// No account scopes this lookup, so the subject exists only to name what a
// failure chats about — the board. parseMcsrBoardArgs is pure, so the request
// reads the remaining flags off the same args rather than routing them through
// a subject with nowhere to keep them.
func mcsrLbRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.McsrLeaderboardReply
	h := mcsrCommand(d, mcsrRoute("leaderboard"), func(cfg mcsrConfig) string { return cfg.LbEnabled },
		func(call statsCall[mcsrConfig], r *reply) string {
			if r.Empty {
				return mcsrBoardLabel(r.Board) + ": " + i18n.T(call.Ctx.Locale, "mcsr.leaderboard.empty")
			}
			return mcsrLbTokens.Expand(orDefault(call.Cfg.LbMessage, defaultMcsrLbTemplate), r)
		})
	// A board, not a player: nothing is resolved, and the subject exists only
	// to name what a failure chats about.
	h.target = func(call statsCall[mcsrConfig]) statsSubject {
		board, _, _ := parseMcsrBoardArgs(call.Args)
		return statsSubject{Display: mcsrBoardLabel(board)}
	}
	return mcsrSeasonRun(h, mcsrBoardSeason)
}

// mcsrBoardSeason is !lb's request: the board selection typed on this call
// plus its season.
func mcsrBoardSeason(season int) mcsrRequest {
	return func(call statsCall[mcsrConfig], _ statsSubject) gossiprpc.Request {
		board, country, predicted := parseMcsrBoardArgs(call.Args)
		return gossiprpc.Request{
			Board:     board,
			Country:   country,
			Predicted: predicted,
			Season:    season,
			IsPremium: call.Ctx.Regress.IsPremium(),
		}
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
var mcsrLbTokens = module.TokenExpander[gossiprpc.McsrLeaderboardReply]{
	"board": func(r *gossiprpc.McsrLeaderboardReply) string { return mcsrBoardLabel(r.Board) },
	"list":  func(r *gossiprpc.McsrLeaderboardReply) string { return mcsrFormatLeaderboard(r.Entries) },
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
// not accept one on this endpoint.
func mcsrRaceRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.McsrWeeklyRaceReply
	return externalCommand[mcsrConfig, reply]{
		route:    mcsrRoute("weekly_race"),
		enabled:  func(cfg mcsrConfig) string { return cfg.RaceEnabled },
		message:  func(cfg mcsrConfig) string { return cfg.RaceMessage },
		fallback: defaultMcsrRaceTemplate,
		tokens:   mcsrRaceTokens,
		special:  mcsrRaceSpecial,
	}.run(d)
}

// mcsrRaceSpecial covers the two answers no template can render: nobody has
// run this week's seed at all, and the queried player has no time on it — the
// leader is still worth chatting there, so it is not silence and not an error.
func mcsrRaceSpecial(call statsCall[mcsrConfig], r *gossiprpc.McsrWeeklyRaceReply) (string, bool) {
	switch {
	case r.Empty:
		return i18n.T(call.Ctx.Locale, "mcsr.race.empty"), true
	case !r.HasPlayer:
		return mcsrRaceLeaderText(r) + " · " + r.Player + ": " + i18n.T(call.Ctx.Locale, "mcsr.race.noplayer"), true
	default:
		return "", false
	}
}

func mcsrRaceLeaderText(r *gossiprpc.McsrWeeklyRaceReply) string {
	return "#1 " + r.LeaderName + " (" + r.LeaderTime + ")"
}

// mcsrRaceTokens resolves !race's template tokens: {leader} {leadertime}
// {player} {time} {rank}.
var mcsrRaceTokens = module.TokenExpander[gossiprpc.McsrWeeklyRaceReply]{
	"leader":     func(r *gossiprpc.McsrWeeklyRaceReply) string { return r.LeaderName },
	"leadertime": func(r *gossiprpc.McsrWeeklyRaceReply) string { return r.LeaderTime },
	"player":     func(r *gossiprpc.McsrWeeklyRaceReply) string { return r.Player },
	"time":       func(r *gossiprpc.McsrWeeklyRaceReply) string { return r.PlayerTime },
	"rank":       func(r *gossiprpc.McsrWeeklyRaceReply) string { return strconv.Itoa(r.PlayerRank) },
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
// mcsr's own season-best for ranked), so each answers from its own handler
// rather than forcing one reply type across both.
//
// The ranked branch reads BestTimeMS off the same "user" endpoint !elo
// already calls — it is fetched there but was unused until !pb ranked needed
// it, so this adds no new upstream call. A 0 BestTimeMS covers both "rated
// but no ranked completion yet" and "unrated" (the upstream never populates
// a season best for either) — both read as the same "no personal best" line.
func mcsrPbRun(d engine.Deps) module.RunFunc {
	ranked, paced := mcsrPbRankedHandler(d), mcsrPbPacemanHandler(d)
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		window, rest := parseMcsrPbArgs(args)
		if window == "ranked" {
			return ranked.run(ctx, c, rest, emit)
		}
		// The window is this call's, not the command's: copy before scoping.
		scoped := paced
		scoped.request = mcsrWindowRequest(window)
		return scoped.run(ctx, c, rest, emit)
	}
}

// mcsrPbRankedHandler answers "!pb ranked" from the MCSR Ranked season best.
func mcsrPbRankedHandler(d engine.Deps) statsHandler[mcsrConfig, gossiprpc.McsrUserReply] {
	type reply = gossiprpc.McsrUserReply
	return mcsrCommand(d, mcsrRoute("user"), func(cfg mcsrConfig) string { return cfg.PbEnabled },
		func(call statsCall[mcsrConfig], r *reply) string {
			if r.BestTimeMS <= 0 {
				return mcsrPbEmptyText(call.Ctx, r.Nickname, "ranked")
			}
			return mcsrPbText(call, mcsrPbView{
				Player:      r.Nickname,
				Time:        mcsrMsToClock(r.BestTimeMS),
				WindowLabel: mcsrPbWindowLabel(call.Ctx, "ranked"),
			})
		})
}

// mcsrPbPacemanHandler answers every other !pb window from PaceMan's own
// precomputed personal bests. Its request is scoped per call (mcsrPbRun) since
// the window is typed, not wired.
func mcsrPbPacemanHandler(d engine.Deps) statsHandler[mcsrConfig, gossiprpc.PacemanPersonalBestReply] {
	type reply = gossiprpc.PacemanPersonalBestReply
	return mcsrCommand(d, pacemanRoute("personal_best"), func(cfg mcsrConfig) string { return cfg.PbEnabled },
		func(call statsCall[mcsrConfig], r *reply) string {
			if r.Empty {
				return mcsrPbEmptyText(call.Ctx, r.Player, r.Window)
			}
			return mcsrPbText(call, mcsrPbView{
				Player:      r.Player,
				Time:        r.Time,
				WindowLabel: mcsrPbWindowLabel(call.Ctx, r.Window),
			})
		})
}

// mcsrWindowRequest is !pb's PaceMan request: the resolved account and the
// window this call asked for.
func mcsrWindowRequest(window string) mcsrRequest {
	return func(call statsCall[mcsrConfig], subject statsSubject) gossiprpc.Request {
		return gossiprpc.Request{Account: subject.Account, TimeWindow: window, IsPremium: call.Ctx.Regress.IsPremium()}
	}
}

// mcsrPbView bundles !pb's three rendered fields so the palette takes one
// named value instead of three interchangeable strings (String Heavy
// Function Arguments) — both !pb branches above build one from their own
// reply shape before handing it over.
type mcsrPbView struct {
	Player      string
	Time        string
	WindowLabel string
}

// mcsrPbText renders !pb's chat line: {player} {time} {window} over the
// broadcaster's template, or the default.
func mcsrPbText(call statsCall[mcsrConfig], v mcsrPbView) string {
	palette := module.StringPalette{
		"player": v.Player,
		"time":   v.Time,
		"window": v.WindowLabel,
	}
	return palette.Expand(orDefault(call.Cfg.PbMessage, defaultMcsrPbTemplate))
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
