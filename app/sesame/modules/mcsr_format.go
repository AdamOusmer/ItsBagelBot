// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
)

// This file holds the small value-rendering and token-map helpers every
// !mcsr/!pace command's Tokens function and mcsrHandler build on: how a
// template token map is looked up and merged, how an elo/rank/split/age
// value is rendered for chat, and how a trailing "season:<n>" argument
// token is parsed. Splitting these out of mcsr.go keeps that file to the
// module's wiring and its shared dispatch shape (mcsrHandler, mcsrSeasonSpec,
// mcsrPaceSpec's common pieces).

// mcsrTokenLookup is the small dispatch every !mcsr/!pace template-token
// resolver shares: check the reply's precomputed key/value map, falling
// back to module.ParseDynamic for any token the reply itself doesn't
// answer. Centralizing it here means each command's Tokens function is
// just a map literal, not a copy of this same switch/fallback shape.
func mcsrTokenLookup(tokens map[string]string, key string) (string, bool) {
	if v, ok := tokens[key]; ok {
		return v, true
	}
	return module.ParseDynamic(key)
}

// mcsrMergeTokens combines several token maps into one, so a Tokens function
// can build its map out of shared fragments (mcsrPlayerEloTokens,
// mcsrWinLossTokens below) instead of repeating their entries.
func mcsrMergeTokens(maps ...map[string]string) map[string]string {
	out := make(map[string]string, len(maps)*4)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// mcsrPlayerEloTokens is the {player}/{elo} token pair !elo and !session
// both answer from a player name and a live elo value.
func mcsrPlayerEloTokens(c *module.Context, player string, elo int) map[string]string {
	return map[string]string{
		"player": player,
		"elo":    mcsrElo(c, elo),
	}
}

// mcsrWinLossTokens is the {wins}/{losses}/{matches} token triple !elo and
// !session both answer from a season (or session) win/loss/played count.
func mcsrWinLossTokens(wins, losses, matches int) map[string]string {
	return map[string]string{
		"wins":    strconv.Itoa(wins),
		"losses":  strconv.Itoa(losses),
		"matches": strconv.Itoa(matches),
	}
}

// mcsrEmptyText answers a lookup that found no data to report (no fortress
// pace this window, no matches played, no personal best set yet): a normal
// PaceMan/MCSR answer, not an error, so it renders one plain translated line
// instead of a template with nothing (or a fake zero) to fill it.
func mcsrEmptyText(c *module.Context, player, key string) string {
	return player + ": " + i18n.T(c.Locale, key)
}

// mcsrElo renders an elo value, naming the unrated sentinel. It takes the
// command's *module.Context rather than a bare locale string — one less
// primitive threaded alongside the many others in this file.
func mcsrElo(c *module.Context, elo int) string {
	if elo < 0 {
		return i18n.T(c.Locale, "mcsr.unrated")
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

// mcsrSplit renders a lastfort/lastmatch split, dashing a run that never
// reached it (the provider answers "" for that case).
func mcsrSplit(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// mcsrAge renders a run's age as a short human duration: enough resolution
// to answer "how stale is this pace/match" without a full clock string.
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
