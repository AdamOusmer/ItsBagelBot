// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
)

func mcsrPlayerElo(c *module.Context, player string, elo int) module.StringPalette {
	return module.StringPalette{
		"player": player,
		"elo":    mcsrElo(c, elo),
	}
}

func mcsrWinLoss(wins, losses, matches int) module.StringPalette {
	draws := matches - wins - losses
	if draws < 0 {
		draws = 0
	}
	return module.StringPalette{
		"wins":    strconv.Itoa(wins),
		"losses":  strconv.Itoa(losses),
		"draws":   strconv.Itoa(draws),
		"matches": strconv.Itoa(matches),
	}
}

func mcsrEmptyText(c *module.Context, player, key string) string {
	return player + ": " + i18n.T(c.Locale, key)
}

func mcsrElo(c *module.Context, elo int) string {
	if elo < 0 {
		return i18n.T(c.Locale, "mcsr.unrated")
	}
	return strconv.Itoa(elo)
}

func mcsrRank(rank int) string {
	if rank < 0 {
		return "—"
	}
	return strconv.Itoa(rank)
}

func mcsrSplit(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

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
