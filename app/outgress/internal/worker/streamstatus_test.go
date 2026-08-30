// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"testing"
	"time"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/projection"
)

func TestNextStreamInfoGoLiveSeedsFromEmpty(t *testing.T) {
	started := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	got := nextStreamInfo(projection.StreamInfo{}, true, twitch.StreamDetails{
		Title: "Ranked grind", GameName: "Fortnite", ViewerCount: 42, StartedAt: started,
	})

	want := projection.StreamInfo{
		Title: "Ranked grind", GameName: "Fortnite", ViewerCount: 42, PeakViewers: 42, StartedAt: started,
	}
	if got != want {
		t.Fatalf("nextStreamInfo() = %+v, want %+v", got, want)
	}
}

func TestNextStreamInfoPeakIsAHighWaterMark(t *testing.T) {
	prev := projection.StreamInfo{PeakViewers: 100, ViewerCount: 40}

	// A sample below the recorded peak must not lower it.
	got := nextStreamInfo(prev, true, twitch.StreamDetails{ViewerCount: 60})
	if got.PeakViewers != 100 {
		t.Fatalf("PeakViewers = %d, want 100 (peak must not drop)", got.PeakViewers)
	}
	if got.ViewerCount != 60 {
		t.Fatalf("ViewerCount = %d, want 60 (current count tracks the latest sample)", got.ViewerCount)
	}

	// A sample above the recorded peak raises it.
	got = nextStreamInfo(prev, true, twitch.StreamDetails{ViewerCount: 150})
	if got.PeakViewers != 150 {
		t.Fatalf("PeakViewers = %d, want 150", got.PeakViewers)
	}
}

func TestNextStreamInfoGoOfflineSetsEndedAtAndKeepsPeak(t *testing.T) {
	started := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	prev := projection.StreamInfo{
		Title: "Ranked grind", GameName: "Fortnite", ViewerCount: 60, PeakViewers: 150, StartedAt: started,
	}

	before := time.Now()
	got := nextStreamInfo(prev, false, twitch.StreamDetails{})
	after := time.Now()

	if got.PeakViewers != 150 || got.Title != "Ranked grind" || !got.StartedAt.Equal(started) {
		t.Fatalf("nextStreamInfo() offline changed retained fields: %+v", got)
	}
	if got.EndedAt.Before(before) || got.EndedAt.After(after) {
		t.Fatalf("EndedAt = %v, want between %v and %v", got.EndedAt, before, after)
	}
}

func TestNextStreamInfoGoLiveClearsPriorEndedAt(t *testing.T) {
	prev := projection.StreamInfo{EndedAt: time.Now().Add(-time.Hour)}

	got := nextStreamInfo(prev, true, twitch.StreamDetails{ViewerCount: 10})
	if !got.EndedAt.IsZero() {
		t.Fatalf("EndedAt = %v, want zero on a fresh go-live", got.EndedAt)
	}
}
