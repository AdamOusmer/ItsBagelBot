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

func mcsrEloRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.McsrUserReply
	h := mcsrCommand(d, mcsrRoute("user"), func(cfg mcsrConfig) string { return cfg.EloEnabled },
		func(call statsCall[mcsrConfig], r *reply) string {
			return mcsrEloPalette(call.Ctx, r).Expand(orDefault(call.Cfg.EloMessage, defaultMcsrEloTemplate))
		})
	return mcsrSeasonRun(h, mcsrAccountSeason)
}

func mcsrEloPalette(c *module.Context, r *gossiprpc.McsrUserReply) module.StringPalette {
	return mcsrPlayerElo(c, r.Nickname, r.Elo).Merge(
		mcsrWinLoss(r.Wins, r.Loses, r.Played),
		module.StringPalette{
			"rank":    mcsrRank(r.Rank),
			"country": r.Country,
		},
	)
}

func mcsrSessionRun(d engine.Deps) module.RunFunc {
	h := mcsrCommand(d, mcsrRoute("session"), func(cfg mcsrConfig) string { return cfg.SessionEnabled }, mcsrSessionText)
	h.request = func(call statsCall[mcsrConfig], subject statsSubject) gossiprpc.Request {
		return gossiprpc.Request{
			Account:   subject.Account,
			ChannelID: strconv.FormatUint(call.Ctx.BroadcasterID, 10),
			IsPremium: call.Ctx.Regress.IsPremium(),
		}
	}
	return ignoreArgs(h.run)
}

func mcsrSessionText(call statsCall[mcsrConfig], r *gossiprpc.McsrSessionReply) string {
	if !r.HasSnapshot {
		started := i18n.T(call.Ctx.Locale, "mcsr.session.started")
		return r.Nickname + ": " + fmt.Sprintf(started, mcsrElo(call.Ctx, r.Elo))
	}
	return mcsrSessionPalette(call.Ctx, r).Expand(mcsrSessionTemplate(call.Cfg.SessionMessage))
}

func mcsrSessionTemplate(stored string) string {
	if strings.TrimSpace(stored) == legacyMcsrSessionTemplate {
		return defaultMcsrSessionTemplate
	}
	return orDefault(stored, defaultMcsrSessionTemplate)
}

func mcsrSessionPalette(c *module.Context, r *gossiprpc.McsrSessionReply) module.StringPalette {
	return mcsrPlayerElo(c, r.Nickname, r.Elo).Merge(
		mcsrWinLoss(r.Wins, r.Loses, r.Played),
		module.StringPalette{"elochange": signed(r.EloChange)},
	)
}

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

func mcsrRecordRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.McsrRecordReply
	h := mcsrCommand(d, mcsrRoute("versus"), func(cfg mcsrConfig) string { return cfg.RecordEnabled },
		func(call statsCall[mcsrConfig], r *reply) string {
			return mcsrRecordTokens.Expand(orDefault(call.Cfg.RecordMessage, defaultMcsrRecordTemplate), r)
		})
	h.target = mcsrRecordSubject
	return mcsrSeasonRun(h, mcsrVersusSeason)
}

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

func mcsrRecordSubject(call statsCall[mcsrConfig]) statsSubject {
	a, b, displayA := mcsrRecordAccounts(call.Args, call.Cfg, call.Ctx)
	if a == "" || b == "" {
		return statsSubject{Refusal: i18n.T(call.Ctx.Locale, "mcsr.record.usage")}
	}
	return statsSubject{Account: a, AccountB: b, Display: displayA}
}

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

var mcsrRecordTokens = module.TokenExpander[gossiprpc.McsrRecordReply]{
	"playera": func(r *gossiprpc.McsrRecordReply) string { return r.PlayerA },
	"playerb": func(r *gossiprpc.McsrRecordReply) string { return r.PlayerB },
	"winsa":   func(r *gossiprpc.McsrRecordReply) string { return strconv.Itoa(r.WinsA) },
	"winsb":   func(r *gossiprpc.McsrRecordReply) string { return strconv.Itoa(r.WinsB) },
	"played":  func(r *gossiprpc.McsrRecordReply) string { return strconv.Itoa(r.Played) },
}

func mcsrLbRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.McsrLeaderboardReply
	h := mcsrCommand(d, mcsrRoute("leaderboard"), func(cfg mcsrConfig) string { return cfg.LbEnabled },
		func(call statsCall[mcsrConfig], r *reply) string {
			if r.Empty {
				return mcsrBoardLabel(r.Board) + ": " + i18n.T(call.Ctx.Locale, "mcsr.leaderboard.empty")
			}
			return mcsrLbTokens.Expand(orDefault(call.Cfg.LbMessage, defaultMcsrLbTemplate), r)
		})
	h.target = func(call statsCall[mcsrConfig]) statsSubject {
		board, _, _ := parseMcsrBoardArgs(call.Args)
		return statsSubject{Display: mcsrBoardLabel(board)}
	}
	return mcsrSeasonRun(h, mcsrBoardSeason)
}

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

var mcsrLbTokens = module.TokenExpander[gossiprpc.McsrLeaderboardReply]{
	"board": func(r *gossiprpc.McsrLeaderboardReply) string { return mcsrBoardLabel(r.Board) },
	"list":  func(r *gossiprpc.McsrLeaderboardReply) string { return mcsrFormatLeaderboard(r.Entries) },
}

func mcsrFormatLeaderboard(entries []gossiprpc.McsrLeaderboardEntry) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, "#"+strconv.Itoa(e.Rank)+" "+e.Name+" "+e.Value)
	}
	return strings.Join(parts, " · ")
}

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

var mcsrRaceTokens = module.TokenExpander[gossiprpc.McsrWeeklyRaceReply]{
	"leader":     func(r *gossiprpc.McsrWeeklyRaceReply) string { return r.LeaderName },
	"leadertime": func(r *gossiprpc.McsrWeeklyRaceReply) string { return r.LeaderTime },
	"player":     func(r *gossiprpc.McsrWeeklyRaceReply) string { return r.Player },
	"time":       func(r *gossiprpc.McsrWeeklyRaceReply) string { return r.PlayerTime },
	"rank":       func(r *gossiprpc.McsrWeeklyRaceReply) string { return strconv.Itoa(r.PlayerRank) },
}

var mcsrPbWindows = map[string]bool{
	"daily":   true,
	"weekly":  true,
	"monthly": true,
	"ranked":  true,
}

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

func mcsrPbRun(d engine.Deps) module.RunFunc {
	ranked, paced := mcsrPbRankedHandler(d), mcsrPbPacemanHandler(d)
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		window, rest := parseMcsrPbArgs(args)
		if window == "ranked" {
			return ranked.run(ctx, c, rest, emit)
		}
		scoped := paced
		scoped.request = mcsrWindowRequest(window)
		return scoped.run(ctx, c, rest, emit)
	}
}

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

func mcsrWindowRequest(window string) mcsrRequest {
	return func(call statsCall[mcsrConfig], subject statsSubject) gossiprpc.Request {
		return gossiprpc.Request{Account: subject.Account, TimeWindow: window, IsPremium: call.Ctx.Regress.IsPremium()}
	}
}

type mcsrPbView struct {
	Player      string
	Time        string
	WindowLabel string
}

func mcsrPbText(call statsCall[mcsrConfig], v mcsrPbView) string {
	palette := module.StringPalette{
		"player": v.Player,
		"time":   v.Time,
		"window": v.WindowLabel,
	}
	return palette.Expand(orDefault(call.Cfg.PbMessage, defaultMcsrPbTemplate))
}

func mcsrPbEmptyText(c *module.Context, player, window string) string {
	return player + ": " + fmt.Sprintf(i18n.T(c.Locale, "mcsr.pb.empty"), mcsrPbWindowLabel(c, window))
}

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

func mcsrMsToClock(ms int64) string {
	if ms <= 0 {
		return ""
	}
	minutes := ms / 60000
	seconds := (ms % 60000) / 1000
	millis := ms % 1000
	return fmt.Sprintf("%d:%02d.%03d", minutes, seconds, millis)
}
