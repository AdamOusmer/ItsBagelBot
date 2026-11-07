// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import (
	"fmt"
	"strings"
	"time"
)

var humanizeUnits = []struct {
	minutes int64
	key     string
}{
	{365 * 24 * 60, "time.year"},
	{30 * 24 * 60, "time.month"},
	{24 * 60, "time.day"},
	{60, "time.hour"},
	{1, "time.minute"},
}

const humanizeParts = 2

func HumanizeDuration(locale string, d time.Duration) string {
	minutes := int64(d / time.Minute)
	if minutes < 1 {
		return T(locale, "time.less_than_minute")
	}
	parts := make([]string, 0, humanizeParts)
	for _, unit := range humanizeUnits {
		n := minutes / unit.minutes
		if n == 0 {
			continue
		}
		parts = append(parts, humanizePart(locale, n, unit.key))
		minutes %= unit.minutes
		if len(parts) == humanizeParts {
			break
		}
	}
	return strings.Join(parts, ", ")
}

var compactUnits = []struct {
	seconds int64
	key     string
}{
	{24 * 3600, "time.short_day"},
	{3600, "time.short_hour"},
	{60, "time.short_minute"},
	{1, "time.short_second"},
}

// CompactDuration reads like 2d 3h, 45m or 12s, rounded up so a running window never shows nothing.
func CompactDuration(locale string, d time.Duration) string {
	if d <= 0 {
		return ""
	}
	seconds := int64((d + time.Second - 1) / time.Second)
	parts := make([]string, 0, humanizeParts)
	for _, unit := range compactUnits {
		n := seconds / unit.seconds
		if n == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d%s", n, T(locale, unit.key)))
		seconds %= unit.seconds
		if len(parts) == humanizeParts {
			break
		}
	}
	return strings.Join(parts, " ")
}

func humanizePart(locale string, n int64, key string) string {
	if n != 1 {
		key += "s"
	}
	return fmt.Sprintf("%d %s", n, T(locale, key))
}
