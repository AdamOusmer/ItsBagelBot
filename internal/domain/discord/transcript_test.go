// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestRenderTranscriptLinesAndAttachments(t *testing.T) {
	got := RenderTranscript(TranscriptDoc{
		ChannelName: "ticket-ada-1",
		Messages: []TranscriptMessage{
			{AuthorName: "Ada", Content: "my sub is missing", At: at("2026-01-02T03:04:05Z")},
			{AuthorName: "", Content: "looking", At: at("2026-01-02T04:00:00Z"),
				Attachments: []string{"https://cdn/a.png", "https://cdn/b.png"}},
		},
	})

	want := "[2026-01-02 03:04 UTC] Ada: my sub is missing\n" +
		"[2026-01-02 04:00 UTC] unknown: looking\n" +
		"    [attachment] https://cdn/a.png\n" +
		"    [attachment] https://cdn/b.png\n"
	if got != want {
		t.Fatalf("transcript =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderTranscriptRendersInUTCWhateverTheInputZone(t *testing.T) {
	zone := time.FixedZone("UTC+9", 9*60*60)
	got := RenderTranscript(TranscriptDoc{Messages: []TranscriptMessage{
		{AuthorName: "Ada", Content: "hi", At: at("2026-01-02T03:04:05Z").In(zone)},
	}})

	if !strings.HasPrefix(got, "[2026-01-02 03:04 UTC]") {
		t.Fatalf("transcript = %q, want the UTC instant regardless of the input zone", got)
	}
}

func TestRenderTranscriptTruncatesTheOldestEnd(t *testing.T) {
	// One line per message, sized so the whole body comfortably exceeds the
	// byte cap; the newest lines are the ones that must survive.
	line := strings.Repeat("x", 512)
	var msgs []TranscriptMessage
	for i := 0; i < (TranscriptByteCap/512)+50; i++ {
		msgs = append(msgs, TranscriptMessage{AuthorName: "a", Content: line, At: at("2026-01-02T03:04:05Z")})
	}
	msgs[0].Content = "OLDEST"
	msgs[len(msgs)-1].Content = "NEWEST"

	got := RenderTranscript(TranscriptDoc{Messages: msgs})

	if len(got) > TranscriptByteCap+len(transcriptTruncated) {
		t.Fatalf("body = %d bytes, over the cap", len(got))
	}
	if !strings.HasPrefix(got, transcriptTruncated) {
		t.Fatal("a truncated body must say so, not leave the reader guessing")
	}
	if strings.Contains(got, "OLDEST") {
		t.Fatal("the oldest end is what gets cut")
	}
	if !strings.Contains(got, "NEWEST") {
		t.Fatal("the tail of the conversation is what explains how it ended")
	}
	// The cut lands on a line boundary: every surviving line is whole.
	for _, l := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(got, transcriptTruncated), "\n"), "\n") {
		if !strings.HasPrefix(l, "[") {
			t.Fatalf("half line survived truncation: %q", l)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{-time.Hour, "0m"},
		{30 * time.Second, "0m"},
		{90 * time.Second, "1m"},
		{59 * time.Minute, "59m"},
		{time.Hour + 5*time.Minute, "1h 5m"},
		{26*time.Hour + 61*time.Minute, "27h 1m"},
	}
	for _, tc := range cases {
		if got := HumanDuration(tc.in); got != tc.want {
			t.Fatalf("HumanDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTicketClosedEmbedCarriesTheAuditFields(t *testing.T) {
	e := TicketClosedEmbed(TicketClosed{
		Opener: "<@u1>", Duration: 95 * time.Minute, MessageCount: 12, ChannelName: "ticket-ada-1",
	})

	if e.Description != "#ticket-ada-1 was closed." {
		t.Fatalf("description = %q", e.Description)
	}
	want := map[string]string{"Opened by": "<@u1>", "Closed by": "unknown", "Open for": "1h 35m", "Messages": "12"}
	for _, f := range e.Fields {
		if expected, ok := want[f.Name]; ok {
			if f.Value != expected {
				t.Fatalf("%s = %q, want %q", f.Name, f.Value, expected)
			}
			delete(want, f.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing fields: %v", want)
	}
}

func TestTicketOpenedEmbedFooterCarriesTheClaim(t *testing.T) {
	unclaimed := TicketOpenedEmbed(TicketOpened{Opener: "Ada"})
	if unclaimed.Footer == nil || !strings.Contains(unclaimed.Footer.Text, "Close with the button") {
		t.Fatalf("unclaimed footer = %+v", unclaimed.Footer)
	}
	claimed := TicketOpenedEmbed(TicketOpened{Opener: "Ada", ClaimedBy: "Mod"})
	if claimed.Footer == nil || claimed.Footer.Text != "Claimed by Mod" {
		t.Fatalf("claimed footer = %+v", claimed.Footer)
	}
}

func TestTicketPanelEmbedUsesTheStreamersCopy(t *testing.T) {
	spec := Config{
		TicketPanelTitle: "Need a hand?", TicketPanelBody: "Ping the mods.",
		TicketPanelColor: "#112233", TicketPanelButton: "Contact staff",
	}.TicketPanel()

	e := TicketPanelEmbed(spec)

	if e.Title != "Need a hand?" || e.Description != "Ping the mods." || e.Color != 0x112233 {
		t.Fatalf("embed = %+v", e)
	}
}

func TestTicketPanelSpecOrDefaultsFillsBlanks(t *testing.T) {
	got := TicketPanelSpec{Title: "Kept"}.OrDefaults()

	if got.Title != "Kept" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Body != TicketPanelBodyDefault || got.Button != TicketPanelButtonDefault || got.Color != LiveColor {
		t.Fatalf("spec = %+v", got)
	}
}
