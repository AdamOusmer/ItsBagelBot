// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "time"

type ResetSchedule string

const (
	ResetOff      ResetSchedule = "off"
	ResetWeekly   ResetSchedule = "weekly"
	ResetMonthly  ResetSchedule = "monthly"
	ResetAnnually ResetSchedule = "annually"
)

func (s ResetSchedule) Enabled() bool {
	switch s {
	case ResetWeekly, ResetMonthly, ResetAnnually:
		return true
	default:
		return false
	}
}

func (s ResetSchedule) doneRetention() time.Duration {
	switch s {
	case ResetWeekly:
		return 8 * 24 * time.Hour
	case ResetMonthly:
		return 32 * 24 * time.Hour
	default:
		return 367 * 24 * time.Hour
	}
}

// ResetPeriodStart is the latest boundary at or before at: Monday, the 1st, or Jan 1 at 00:00 in loc.
func ResetPeriodStart(s ResetSchedule, at time.Time, loc *time.Location) time.Time {
	t := at.In(loc)
	year, month, day := t.Date()
	switch s {
	case ResetWeekly:
		return time.Date(year, month, day-int((t.Weekday()+6)%7), 0, 0, 0, 0, loc)
	case ResetMonthly:
		return time.Date(year, month, 1, 0, 0, 0, 0, loc)
	default:
		return time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
	}
}

// NextResetBoundary is the first boundary strictly after at.
func NextResetBoundary(s ResetSchedule, at time.Time, loc *time.Location) time.Time {
	start := ResetPeriodStart(s, at, loc)
	switch s {
	case ResetWeekly:
		return start.AddDate(0, 0, 7)
	case ResetMonthly:
		return start.AddDate(0, 1, 0)
	default:
		return start.AddDate(1, 0, 0)
	}
}
