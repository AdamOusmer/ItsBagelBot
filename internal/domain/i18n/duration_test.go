// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import (
	"testing"
	"time"
)

func TestHumanizeDuration(t *testing.T) {
	cases := []struct {
		name   string
		locale string
		d      time.Duration
		want   string
	}{
		{"two largest units", "en", (2*365 + 3*30 + 4) * 24 * time.Hour, "2 years, 3 months"},
		{"singular unit reads the singular key", "en", 25 * time.Hour, "1 day, 1 hour"},
		{"one unit only", "en", 90 * time.Minute, "1 hour, 30 minutes"},
		{"sub-minute", "en", 30 * time.Second, "less than a minute"},
		{"zero", "en", 0, "less than a minute"},
		{"negative reads as zero", "en", -5 * time.Hour, "less than a minute"},
		{"localized units", "fr", 25 * time.Hour, "1 jour, 1 heure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HumanizeDuration(tc.locale, tc.d); got != tc.want {
				t.Errorf("HumanizeDuration(%q, %v) = %q, want %q", tc.locale, tc.d, got, tc.want)
			}
		})
	}
}
