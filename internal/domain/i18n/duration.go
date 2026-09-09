// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import (
	"fmt"
	"strings"
	"time"
)

// humanizeUnits are the spans HumanizeDuration is allowed to name, largest
// first. A month is 30 days and a year 365: the caller is writing a chat line
// ("followed 2 years, 3 months ago"), not an invoice, so a calendar-exact
// difference would cost a civil-date library to move the last digit of a
// number nobody reads that closely.
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

// humanizeParts is how many units one rendered span may name. Two reads as a
// sentence ("2 years, 3 months") and stays inside a chat line; three starts
// reading as a stopwatch.
const humanizeParts = 2

// HumanizeDuration renders a span as the two largest non-zero units (e.g.
// "2 years, 3 months"). Unit names are localized; a plural count reads the
// "<unit>s" key ("time.year" -> "time.years"). A negative or sub-minute span
// reads as "less than a minute".
//
// It lives here, beside the catalog it reads, rather than in the sesame
// modules package where it was born: !followage, !accountage and !uptime were
// its only callers until the {countdown}/{countup} command tokens needed the
// same wording, and those live in app/twitch/sesame/engine/scope, which the
// modules package imports (through engine) and therefore cannot be imported
// back from. The alternative — a second formatter in the scope package — was
// rejected outright: two clocks that disagree about what "1 day" reads like
// is exactly the drift the shared catalog exists to prevent.
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

// humanizePart renders one "<n> <unit>" pair, reading the plural key when the
// count is not exactly one.
func humanizePart(locale string, n int64, key string) string {
	if n != 1 {
		key += "s"
	}
	return fmt.Sprintf("%d %s", n, T(locale, key))
}
