// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import (
	"testing"
	"time"
)

func TestCompactDurationKeepsTheTwoLargestUnits(t *testing.T) {
	for want, d := range map[string]time.Duration{
		"":       0,
		"12s":    12 * time.Second,
		"1s":     time.Millisecond,
		"45m":    45 * time.Minute,
		"2d 3h":  51*time.Hour + 20*time.Minute,
		"1h 5s":  time.Hour + 5*time.Second,
		"30d":    30 * 24 * time.Hour,
		"3h 12m": 3*time.Hour + 12*time.Minute + 9*time.Second,
	} {
		if got := CompactDuration(DefaultLocale, d); got != want {
			t.Errorf("CompactDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
