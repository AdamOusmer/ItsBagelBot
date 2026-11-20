// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

const valModuleName = "valorant"

const valCooldown = 10 * time.Second

const (
	defaultValRankTemplate    = "{valorant:player} · {valorant:tier} · {valorant:rr} RR ({valorant:lastchange}) · peak {valorant:peaktier}"
	defaultValRecordTemplate  = "{valorant:player} · {valorant:games} ranked games · {valorant:wins} wins ({valorant:winrate}%)"
	defaultValMatchesTemplate = "{valorant:player}'s last {valorant:count}: {valorant:matches}"
	defaultValAccountTemplate = "{valorant:player} · account level {valorant:level}"
	defaultValBoardTemplate   = "{valorant:board}: {valorant:entries}"
)

const (
	valUnrankedText   = "has no competitive record this act"
	valNoRecordText   = "has no ranked games on record"
	valNoMatchesText  = "has no recent competitive games"
	valEmptyBoardText = "leaderboard has no entries yet"
)

type valorantConfig struct {
	Region   string `json:"region"`
	Platform string `json:"platform"`

	RankEnabled  string `json:"rankEnabled"`
	RankMessage  string `json:"rankMessage"`
	RecEnabled   string `json:"recordEnabled"`
	RecMessage   string `json:"recordMessage"`
	MatchEnabled string `json:"matchesEnabled"`
	MatchMessage string `json:"matchesMessage"`
	AcctEnabled  string `json:"accountEnabled"`
	AcctMessage  string `json:"accountMessage"`
	BoardEnabled string `json:"boardEnabled"`
	BoardMessage string `json:"boardMessage"`
}

func valRankSpecial(_ statsCall[valorantConfig], r *gossiprpc.ValorantRankReply) (string, bool) {
	if !r.Unranked {
		return "", false
	}
	return r.Player + " " + valUnrankedText, true
}

func valRecordSpecial(_ statsCall[valorantConfig], r *gossiprpc.ValorantRecordReply) (string, bool) {
	if !r.Empty {
		return "", false
	}
	return r.Player + " " + valNoRecordText, true
}

func valMatchSpecial(_ statsCall[valorantConfig], r *gossiprpc.ValorantMatchesReply) (string, bool) {
	if !r.Empty {
		return "", false
	}
	return r.Player + " " + valNoMatchesText, true
}

func valBoardSpecial(_ statsCall[valorantConfig], r *gossiprpc.ValorantLeaderboardReply) (string, bool) {
	if !r.Empty {
		return "", false
	}
	return r.Board + " " + valEmptyBoardText, true
}

func valRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: "valorant", Endpoint: endpoint}
}

func newValRuns(d engine.Deps) valRuns {
	return valRuns{
		rank: valRun(d, externalCommand[valorantConfig, gossiprpc.ValorantRankReply]{
			route:    valRoute("rank"),
			enabled:  func(c valorantConfig) string { return c.RankEnabled },
			message:  func(c valorantConfig) string { return c.RankMessage },
			fallback: defaultValRankTemplate,
			tokens:   valRankTokens(),
			special:  valRankSpecial,
		}),
		record: valRun(d, externalCommand[valorantConfig, gossiprpc.ValorantRecordReply]{
			route:    valRoute("record"),
			enabled:  func(c valorantConfig) string { return c.RecEnabled },
			message:  func(c valorantConfig) string { return c.RecMessage },
			fallback: defaultValRecordTemplate,
			tokens:   valRecordTokens(),
			special:  valRecordSpecial,
		}),
		matches: valRun(d, externalCommand[valorantConfig, gossiprpc.ValorantMatchesReply]{
			route:    valRoute("matches"),
			enabled:  func(c valorantConfig) string { return c.MatchEnabled },
			message:  func(c valorantConfig) string { return c.MatchMessage },
			fallback: defaultValMatchesTemplate,
			tokens:   valMatchTokens(),
			special:  valMatchSpecial,
		}),
		account: valRun(d, externalCommand[valorantConfig, gossiprpc.ValorantAccountReply]{
			route:    valRoute("account"),
			enabled:  func(c valorantConfig) string { return c.AcctEnabled },
			message:  func(c valorantConfig) string { return c.AcctMessage },
			fallback: defaultValAccountTemplate,
			tokens:   valAccountTokens(),
		}),
		board: valRun(d, externalCommand[valorantConfig, gossiprpc.ValorantLeaderboardReply]{
			route:    valRoute("leaderboard"),
			enabled:  func(c valorantConfig) string { return c.BoardEnabled },
			message:  func(c valorantConfig) string { return c.BoardMessage },
			fallback: defaultValBoardTemplate,
			tokens:   valBoardTokens(),
			special:  valBoardSpecial,
		}),
	}
}

func Valorant(d engine.Deps) module.Module {
	runs := newValRuns(d)

	m := module.NewModule(valModuleName, module.KindOptIn)
	m.Command("val").Everyone().Cooldown(valCooldown).
		Run(subDispatch(runs.rank, map[string]module.RunFunc{
			"match": runs.matches, "matches": runs.matches, "history": runs.matches,
			"account": runs.account, "who": runs.account,
			"lb": runs.board, "leaderboard": runs.board, "top": runs.board,
			"rank": runs.rank, "standing": runs.rank,
			"record": runs.record,
		}))
	m.Command("valrank").Everyone().Cooldown(valCooldown).
		Run(runs.rank)
	m.Command("valrecord").Everyone().Cooldown(valCooldown).
		Run(runs.record)
	m.Command("valmatches").Everyone().Cooldown(valCooldown).Aliases("valhistory").
		Run(runs.matches)
	m.Command("valaccount").Everyone().Cooldown(valCooldown).Aliases("valwho").
		Run(runs.account)
	m.Command("vallb").Everyone().Cooldown(valCooldown).Aliases("valleaderboard").
		Run(runs.board)
	return m.Build()
}

type valRuns struct {
	rank, record, matches, account, board module.RunFunc
}

func (valorantConfig) linked() linkedAccountConfig { return linkedAccountConfig{} }

func valRun[R any](d engine.Deps, cmd externalCommand[valorantConfig, R]) module.RunFunc {
	return statsHandler[valorantConfig, R]{
		d:       d,
		enabled: cmd.enabled,
		route:   cmd.route,
		target: func(call statsCall[valorantConfig]) statsSubject {
			return statsSubject{Display: call.Ctx.Env.BroadcasterUserLogin}
		},
		request: func(call statsCall[valorantConfig], _ statsSubject) gossiprpc.Request {
			return valLinkedRequest(call)
		},
		render: cmd.render,
	}.run
}

func valLinkedRequest(call statsCall[valorantConfig]) gossiprpc.Request {
	region, platform := parseValArgs(call.Args)
	return gossiprpc.Request{
		ChannelID: strconv.FormatUint(call.Ctx.BroadcasterID, 10),
		Region:    orDefault(region, call.Cfg.Region),
		Platform:  orDefault(platform, call.Cfg.Platform),
		IsPremium: call.Ctx.Regress.IsPremium(),
	}
}

func parseValArgs(args string) (region, platform string) {
	for _, f := range strings.Fields(args) {
		switch w := strings.ToLower(f); w {
		case "na", "eu", "ap", "kr", "br", "latam":
			region = w
		case "pc", "console":
			platform = w
		}
	}
	return region, platform
}

func valRankTokens() module.TokenExpander[gossiprpc.ValorantRankReply] {
	type reply = gossiprpc.ValorantRankReply
	return module.TokenExpander[reply]{
		"player":     func(r *reply) string { return r.Player },
		"region":     func(r *reply) string { return r.Region },
		"tier":       func(r *reply) string { return r.Tier },
		"elo":        func(r *reply) string { return i64(int64(r.Elo)) },
		"rr":         func(r *reply) string { return i64(int64(r.RR)) },
		"lastchange": func(r *reply) string { return signed(r.LastChange) },
		"peaktier":   func(r *reply) string { return r.PeakTier },
		"placement":  func(r *reply) string { return i64(int64(r.Placement)) },
	}
}

func valRecordTokens() module.TokenExpander[gossiprpc.ValorantRecordReply] {
	type reply = gossiprpc.ValorantRecordReply
	return module.TokenExpander[reply]{
		"player":  func(r *reply) string { return r.Player },
		"region":  func(r *reply) string { return r.Region },
		"games":   func(r *reply) string { return i64(int64(r.Games)) },
		"wins":    func(r *reply) string { return i64(int64(r.Wins)) },
		"winrate": func(r *reply) string { return i64(valWinRate(r.Wins, r.Games)) },
	}
}

func valWinRate(wins, games int) int64 {
	if games <= 0 {
		return 0
	}
	return int64(math.Round(float64(wins) * 100 / float64(games)))
}

func valMatchTokens() module.TokenExpander[gossiprpc.ValorantMatchesReply] {
	type reply = gossiprpc.ValorantMatchesReply
	return module.TokenExpander[reply]{
		"player": func(r *reply) string { return r.Player },
		"region": func(r *reply) string { return r.Region },
		"count":  func(r *reply) string { return i64(int64(len(r.Matches))) },
		"matches": func(r *reply) string {
			parts := make([]string, len(r.Matches))
			for i, m := range r.Matches {
				parts[i] = fmt.Sprintf("%s %d/%d/%d %s on %s", m.Agent, m.Kills, m.Deaths, m.Assists, m.Result, m.Map)
			}
			return strings.Join(parts, ", ")
		},
		"lastago": func(r *reply) string {
			if m := r.Matches; len(m) > 0 {
				return valAgo(m[0].AgoSeconds)
			}
			return ""
		},
	}
}

type valAccount = gossiprpc.ValorantAccountReply

var valAccountTokenSet = module.TokenExpander[valAccount]{
	"player": func(r *valAccount) string { return r.Player },
	"puuid":  func(r *valAccount) string { return r.Puuid },
	"region": func(r *valAccount) string { return r.Region },
	"level":  func(r *valAccount) string { return i64(int64(r.AccountLevel)) },
	"card":   func(r *valAccount) string { return r.Card },
	"title":  func(r *valAccount) string { return r.Title },
}

func valAccountTokens() module.TokenExpander[valAccount] {
	return valAccountTokenSet
}

func valBoardTokens() module.TokenExpander[gossiprpc.ValorantLeaderboardReply] {
	type reply = gossiprpc.ValorantLeaderboardReply
	return module.TokenExpander[reply]{
		"player": func(r *reply) string { return r.Player },
		"board":  func(r *reply) string { return r.Board },
		"count":  func(r *reply) string { return i64(int64(len(r.Entries))) },
		"entries": func(r *reply) string {
			entries := r.Entries
			parts := make([]string, len(entries))
			for i, e := range entries {
				parts[i] = "#" + i64(int64(e.Rank)) + " " + e.Player + " (" + i64(int64(e.RR)) + " RR)"
			}
			return strings.Join(parts, ", ")
		},
	}
}

func valAgo(seconds int64) string {
	d := time.Duration(seconds) * time.Second
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
