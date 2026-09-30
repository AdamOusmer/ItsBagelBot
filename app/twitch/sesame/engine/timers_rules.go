// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "time"

const (
	maxGateLines = 100
	maxFireCap   = 100

	defaultChatWindowMinutes = 5
	maxChatWindowMinutes     = 60
)

func chatWindow(td timerDef) time.Duration {
	switch {
	case td.ChatWindowMinutes <= 0:
		return defaultChatWindowMinutes * time.Minute
	case td.ChatWindowMinutes > maxChatWindowMinutes:
		return maxChatWindowMinutes * time.Minute
	default:
		return time.Duration(td.ChatWindowMinutes) * time.Minute
	}
}

func clampCount(n, limit int) int {
	switch {
	case n < 0:
		return 0
	case n > limit:
		return limit
	default:
		return n
	}
}

func isGated(td timerDef) bool {
	return clampCount(td.MinChatLines, maxGateLines) > 0
}

func gatePasses(td timerDef, lines int64) bool {
	return lines >= int64(clampCount(td.MinChatLines, maxGateLines))
}

func stopped(td timerDef, fires int64, now time.Time) bool {
	if fireCap := clampCount(td.MaxFires, maxFireCap); fireCap > 0 && fires >= int64(fireCap) {
		return true
	}
	endsAt, ok := parseEndsAt(td.EndsAt)
	return ok && !now.Before(endsAt)
}

func parseEndsAt(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
