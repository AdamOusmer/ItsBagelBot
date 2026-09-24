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

func humanizePart(locale string, n int64, key string) string {
	if n != 1 {
		key += "s"
	}
	return fmt.Sprintf("%d %s", n, T(locale, key))
}
