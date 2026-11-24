// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"fmt"
	"time"
)

// Period is a tally window. Day, week and month are UTC calendar windows (ISO weeks); a session window is named by its session id.
type Period string

const (
	PeriodSession Period = "session"
	PeriodDay     Period = "day"
	PeriodWeek    Period = "week"
	PeriodMonth   Period = "month"
)

// Window names the period's window holding at; session is the id used for PeriodSession.
func (p Period) Window(at time.Time, session string) string {
	at = at.UTC()
	switch p {
	case PeriodDay:
		return "d" + at.Format("20060102")
	case PeriodWeek:
		year, week := at.ISOWeek()
		return fmt.Sprintf("w%04d%02d", year, week)
	case PeriodMonth:
		return "m" + at.Format("200601")
	}
	return "s" + session
}

// Key joins prefix and the window name.
func (p Period) Key(prefix string, at time.Time, session string) string {
	return prefix + ":" + p.Window(at, session)
}

// TTL outlives the window so a late fold still lands before expiry.
func (p Period) TTL() time.Duration {
	switch p {
	case PeriodDay:
		return 48 * time.Hour
	case PeriodWeek:
		return 9 * 24 * time.Hour
	case PeriodMonth:
		return 35 * 24 * time.Hour
	}
	return 72 * time.Hour
}
