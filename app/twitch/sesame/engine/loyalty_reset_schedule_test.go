// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResetBoundaries(t *testing.T) {
	tokyo := time.FixedZone("UTC+9", 9*3600)
	at := func(layout string, loc *time.Location) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02 15:04:05", layout, loc)
		assert.NoError(t, err)
		return parsed
	}
	cases := []struct {
		name     string
		schedule ResetSchedule
		now      time.Time
		loc      *time.Location
		start    time.Time
		next     time.Time
	}{
		{"weekly midweek", ResetWeekly, at("2026-10-21 12:00:00", time.UTC), time.UTC, at("2026-10-19 00:00:00", time.UTC), at("2026-10-26 00:00:00", time.UTC)},
		{"weekly on monday", ResetWeekly, at("2026-10-19 00:00:00", time.UTC), time.UTC, at("2026-10-19 00:00:00", time.UTC), at("2026-10-26 00:00:00", time.UTC)},
		{"weekly sunday night", ResetWeekly, at("2026-10-25 23:59:59", time.UTC), time.UTC, at("2026-10-19 00:00:00", time.UTC), at("2026-10-26 00:00:00", time.UTC)},
		{"weekly across month", ResetWeekly, at("2026-12-31 08:00:00", time.UTC), time.UTC, at("2026-12-28 00:00:00", time.UTC), at("2027-01-04 00:00:00", time.UTC)},
		{"monthly december rollover", ResetMonthly, at("2026-12-15 10:00:00", time.UTC), time.UTC, at("2026-12-01 00:00:00", time.UTC), at("2027-01-01 00:00:00", time.UTC)},
		{"monthly february", ResetMonthly, at("2028-02-29 23:00:00", time.UTC), time.UTC, at("2028-02-01 00:00:00", time.UTC), at("2028-03-01 00:00:00", time.UTC)},
		{"annually", ResetAnnually, at("2026-12-31 23:59:59", time.UTC), time.UTC, at("2026-01-01 00:00:00", time.UTC), at("2027-01-01 00:00:00", time.UTC)},
		{"monthly in zone ahead of utc", ResetMonthly, at("2026-12-31 16:00:00", time.UTC), tokyo, at("2027-01-01 00:00:00", tokyo), at("2027-02-01 00:00:00", tokyo)},
		{"weekly in zone ahead of utc", ResetWeekly, at("2026-10-18 16:00:00", time.UTC), tokyo, at("2026-10-19 00:00:00", tokyo), at("2026-10-26 00:00:00", tokyo)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, tc.start.Equal(ResetPeriodStart(tc.schedule, tc.now, tc.loc)), "start")
			assert.True(t, tc.next.Equal(NextResetBoundary(tc.schedule, tc.now, tc.loc)), "next")
		})
	}
}

func TestResetScheduleEnabled(t *testing.T) {
	for _, on := range []ResetSchedule{ResetWeekly, ResetMonthly, ResetAnnually} {
		assert.True(t, on.Enabled(), on)
	}
	for _, off := range []ResetSchedule{ResetOff, "", "daily"} {
		assert.False(t, off.Enabled(), off)
	}
}
