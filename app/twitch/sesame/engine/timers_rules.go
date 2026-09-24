// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "time"

const (
	maxGateLines = 100
	maxFireCap   = 100
)

func clampGateLines(n int) int {
	switch {
	case n < 0:
		return 0
	case n > maxGateLines:
		return maxGateLines
	default:
		return n
	}
}

func clampFireCap(n int) int {
	switch {
	case n < 0:
		return 0
	case n > maxFireCap:
		return maxFireCap
	default:
		return n
	}
}

func isGated(td timerDef) bool {
	return clampGateLines(td.MinChatLines) > 0
}

func gatePasses(td timerDef, lines, mark int64) bool {
	threshold := clampGateLines(td.MinChatLines)
	if threshold <= 0 {
		return true
	}
	return lines-mark >= int64(threshold)
}

func stopped(td timerDef, fires int64, now time.Time) bool {
	if fireCap := clampFireCap(td.MaxFires); fireCap > 0 && fires >= int64(fireCap) {
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
