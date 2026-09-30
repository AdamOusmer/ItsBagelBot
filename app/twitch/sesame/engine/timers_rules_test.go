// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"
	"time"
)

func TestClampCount(t *testing.T) {
	const limit = 100
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"off", 0, 0},
		{"in range", limit / 2, limit / 2},
		{"negative floors to zero", -3, 0},
		{"at ceiling", limit, limit},
		{"above ceiling", limit + 50, limit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clampCount(c.in, limit); got != c.want {
				t.Fatalf("clampCount(%d, %d) = %d, want %d", c.in, limit, got, c.want)
			}
		})
	}
}

func TestIsGated(t *testing.T) {
	if isGated(timerDef{MinChatLines: 0}) {
		t.Fatal("zero threshold must not be gated")
	}
	if !isGated(timerDef{MinChatLines: 1}) {
		t.Fatal("any positive threshold must be gated")
	}
	if isGated(timerDef{MinChatLines: -5}) {
		t.Fatal("a clamped-to-zero threshold must not be gated")
	}
}

func TestGatePasses(t *testing.T) {
	cases := []struct {
		name     string
		minLines int
		lines    int64
		want     bool
	}{
		{"no gate always passes", 0, 0, true},
		{"one short skips", 5, 4, false},
		{"exactly at threshold fires", 5, 5, true},
		{"above threshold fires", 5, 7, true},
		{"threshold clamps to the ceiling", 500, maxGateLines, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			td := timerDef{MinChatLines: c.minLines}
			if got := gatePasses(td, c.lines); got != c.want {
				t.Fatalf("gatePasses(minLines=%d, lines=%d) = %v, want %v", c.minLines, c.lines, got, c.want)
			}
		})
	}
}

func TestChatWindow(t *testing.T) {
	cases := []struct {
		name    string
		minutes int
		want    time.Duration
	}{
		{"missing defaults", 0, 5 * time.Minute},
		{"negative defaults", -3, 5 * time.Minute},
		{"one minute", 1, time.Minute},
		{"at ceiling", 60, time.Hour},
		{"above ceiling clamps", 61, time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chatWindow(timerDef{ChatWindowMinutes: c.minutes}); got != c.want {
				t.Fatalf("chatWindow(%d) = %s, want %s", c.minutes, got, c.want)
			}
		})
	}
}

func TestStoppedByFireCap(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name     string
		maxFires int
		fires    int64
		want     bool
	}{
		{"unlimited never stops", 0, 1000, false},
		{"below cap keeps going", 3, 2, false},
		{"at cap stops", 3, 3, true},
		{"past cap stays stopped", 3, 4, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			td := timerDef{MaxFires: c.maxFires}
			if got := stopped(td, c.fires, now); got != c.want {
				t.Fatalf("stopped(maxFires=%d, fires=%d) = %v, want %v", c.maxFires, c.fires, got, c.want)
			}
		})
	}
}

func TestStoppedByEndDate(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		endsAt string
		want   bool
	}{
		{"never ends", "", false},
		{"unparsable never ends", "not-a-date", false},
		{"future end date has not stopped", "2026-09-21T13:00:00Z", false},
		{"past end date has stopped", "2026-09-21T11:00:00Z", true},
		{"exactly now has stopped", "2026-09-21T12:00:00Z", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			td := timerDef{EndsAt: c.endsAt}
			if got := stopped(td, 0, now); got != c.want {
				t.Fatalf("stopped(endsAt=%q, now=%s) = %v, want %v", c.endsAt, now, got, c.want)
			}
		})
	}
}

func TestStoppedCapAndEndDateCompose(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	td := timerDef{MaxFires: 3, EndsAt: "2026-09-21T13:00:00Z"}

	if stopped(td, 2, now) {
		t.Fatal("under cap and before end date: must not be stopped")
	}
	if !stopped(td, 3, now) {
		t.Fatal("cap reached before end date: must be stopped")
	}

	afterEnd := now.Add(2 * time.Hour)
	if !stopped(td, 0, afterEnd) {
		t.Fatal("past end date even under cap: must be stopped")
	}
}

func TestParseEndsAt(t *testing.T) {
	if _, ok := parseEndsAt(""); ok {
		t.Fatal("empty string must report ok=false (never ends)")
	}
	if _, ok := parseEndsAt("2026-13-99"); ok {
		t.Fatal("garbage must report ok=false, not panic or error out")
	}
	got, ok := parseEndsAt("2026-09-21T12:00:00Z")
	if !ok {
		t.Fatal("a valid RFC 3339 instant must parse")
	}
	want := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseEndsAt = %s, want %s", got, want)
	}
}
