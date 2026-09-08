// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/projection"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
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

	// Each retained field asserted on its own, so a failure names the field
	// that moved instead of reporting that one of three did.
	retained := []struct {
		field string
		kept  bool
	}{
		{"PeakViewers", got.PeakViewers == 150},
		{"Title", got.Title == "Ranked grind"},
		{"StartedAt", got.StartedAt.Equal(started)},
	}
	for _, r := range retained {
		if !r.kept {
			t.Fatalf("nextStreamInfo() offline changed %s: %+v", r.field, got)
		}
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

// TestStreamStatusFailureAcksPermanentRejections pins which failures ack-drop a
// stream_status job and which nack for redelivery. It exercises the classifier
// through streamStatusFailure rather than through processStreamStatus because
// w.live is a concrete *LiveWriter holding a real valkey.Client: injecting a
// failing writer would mean either an interface this code does not otherwise
// need or a live Valkey server in unit tests, and neither buys anything the
// classifier table does not already prove.
//
// The ValkeyError cases use the zero value on purpose: valkey.ValkeyError's
// fields are unexported, so a test outside that package cannot build one
// carrying a message. Only its TYPE matters here, which is exactly what
// errors.As matches on.
func TestStreamStatusFailureAcksPermanentRejections(t *testing.T) {
	w := &Worker{log: zap.NewNop()}

	tests := []struct {
		name     string
		err      error
		wantNack bool
	}{
		{name: "valkey server rejection", err: &valkey.ValkeyError{}},
		{name: "wrapped valkey server rejection", err: fmt.Errorf("live write: %w", &valkey.ValkeyError{})},
		{name: "twitch 4xx", err: &twitch.StatusError{Status: http.StatusBadRequest}},
		{name: "valkey connection timeout", err: context.DeadlineExceeded, wantNack: true},
		{name: "twitch rate limit", err: &twitch.StatusError{Status: http.StatusTooManyRequests}, wantNack: true},
		{name: "twitch 5xx", err: &twitch.StatusError{Status: http.StatusBadGateway}, wantNack: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.streamStatusFailure(context.Background(), "1234", tt.err)
			if (got != nil) != tt.wantNack {
				t.Fatalf("streamStatusFailure(%v) = %v, want nack=%v", tt.err, got, tt.wantNack)
			}
		})
	}
}
