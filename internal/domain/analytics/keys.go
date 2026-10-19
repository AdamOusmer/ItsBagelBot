// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import (
	"strconv"
	"strings"
	"time"
)

// web/kit/lib/server/analytics.ts writes the counter layout; change both together.
const (
	keyPrefix   = "an:"
	fieldSep    = "|"
	retainAfter = 7 * 24 * time.Hour
	saltGrace   = time.Hour
)

func Period(w Window, t time.Time) string {
	t = t.UTC()
	switch w {
	case Week:
		year, week := t.ISOWeek()
		return strconv.Itoa(year) + "-W" + twoDigits(week)
	case Month:
		return t.Format("200601")
	default:
		return t.Format("20060102")
	}
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func PeriodStart(w Window, t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	switch w {
	case Week:
		offset := (int(day.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -offset)
	case Month:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return day
	}
}

func PeriodEnd(w Window, t time.Time) time.Time {
	start := PeriodStart(w, t)
	switch w {
	case Week:
		return start.AddDate(0, 0, 7)
	case Month:
		return start.AddDate(0, 1, 0)
	default:
		return start.AddDate(0, 0, 1)
	}
}

func windowCode(w Window) string {
	switch w {
	case Week:
		return "w"
	case Month:
		return "m"
	default:
		return "d"
	}
}

func CounterKey(day time.Time) string { return keyPrefix + "c:" + Period(Day, day) }

func GaugeKey(day time.Time) string { return keyPrefix + "g:" + Period(Day, day) }

func DistinctKey(w Window, t time.Time, field string) string {
	return keyPrefix + "h" + windowCode(w) + ":" + Period(w, t) + ":" + field
}

// Element salts rotate per active week, so a channel is not linkable across cells.
func CohortKey(signup, active time.Time) string {
	return DistinctKey(Week, active, Field(CohortActive, Period(Week, signup)))
}

// ParseDistinctKey returns the field of a key built by DistinctKey.
func ParseDistinctKey(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, keyPrefix+"h")
	if !ok || len(rest) < 2 {
		return "", false
	}
	_, field, ok := strings.Cut(rest[2:], ":")
	return field, ok
}

func IndexKey(w Window, t time.Time) string {
	return keyPrefix + "i:" + windowCode(w) + ":" + Period(w, t)
}

func SaltKey(w Window, t time.Time) string {
	return keyPrefix + "salt:" + windowCode(w) + ":" + Period(w, t)
}

func RollupClaimKey(w Window, t time.Time) string {
	return keyPrefix + "roll:" + windowCode(w) + ":" + Period(w, t)
}

func WindowCode(w Window) string { return windowCode(w) }

// Data keys outlive their period so a late rollup still finds them.
func DataExpiry(w Window, t time.Time) time.Time { return PeriodEnd(w, t).Add(retainAfter) }

// Salts die right after their period; afterwards nobody can test a known id against the sketch.
func SaltExpiry(w Window, t time.Time) time.Time { return PeriodEnd(w, t).Add(saltGrace) }

func Field(m Metric, dims ...string) string {
	return strings.Join(append([]string{string(m)}, dims...), fieldSep)
}

func ParseField(field string) (Metric, []string) {
	parts := strings.Split(field, fieldSep)
	return Metric(parts[0]), parts[1:]
}
