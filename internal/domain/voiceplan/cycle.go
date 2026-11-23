// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voiceplan

import "time"

type Window struct {
	Key      string
	ResetsAt time.Time
}

func CalendarWindow(now time.Time) Window {
	return Window{Key: Period(now), ResetsAt: ResetsAt(now)}
}

func CycleWindow(now, renewsAt time.Time) Window {
	now, renewsAt = now.UTC(), renewsAt.UTC()
	back := monthsBetween(now, renewsAt)
	for addMonths(renewsAt, -back).After(now) {
		back++
	}
	for !addMonths(renewsAt, 1-back).After(now) {
		back--
	}
	start := addMonths(renewsAt, -back)
	return Window{Key: start.Format(time.DateOnly), ResetsAt: addMonths(renewsAt, 1-back)}
}

func monthsBetween(from, to time.Time) int {
	return (to.Year()-from.Year())*12 + int(to.Month()) - int(from.Month())
}

func addMonths(anchor time.Time, months int) time.Time {
	first := time.Date(anchor.Year(), anchor.Month()+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	hour, minute, second := anchor.Clock()
	return time.Date(first.Year(), first.Month(), min(anchor.Day(), last), hour, minute, second, anchor.Nanosecond(), time.UTC)
}
