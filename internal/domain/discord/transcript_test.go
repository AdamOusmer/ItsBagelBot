// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
	if got.Body != TicketPanelBodyDefault || got.Button != TicketPanelButtonDefault || got.ColorOr(0) != LiveColor {
		t.Fatalf("spec = %+v", got)
	}
}

// TestTicketPanelSpecKeepsABlackColour is the reason Color is a pointer. Black
// is a colour a streamer can pick, and the old "zero means unset" rule
// silently repainted the panel brand purple on every repost.
func TestTicketPanelSpecKeepsABlackColour(t *testing.T) {
	black := 0
	got := TicketPanelSpec{Color: &black}.OrDefaults()
	if got.ColorOr(LiveColor) != 0 {
		t.Fatalf("color = %#x, want #000000 to survive OrDefaults", got.ColorOr(LiveColor))
	}
	if e := TicketPanelEmbed(got); e.Color != 0 {
		t.Fatalf("embed color = %#x, want the black the streamer picked", e.Color)
	}

	// An absent colour is still the one case that defaults.
	unset := TicketPanelSpec{}.OrDefaults()
	if unset.Color == nil || *unset.Color != LiveColor {
		t.Fatalf("unset color = %v, want the brand default", unset.Color)
	}

	// And a config that names black parses to black, not to "unset".
	fromConfig := Config{TicketPanelColor: "#000000"}.TicketPanel()
	if fromConfig.ColorOr(LiveColor) != 0 {
		t.Fatalf("config color = %#x, want 0", fromConfig.ColorOr(LiveColor))
	}
}

// A message body cannot forge a transcript header: every line after the first
// is indented, and the renderer only ever writes a header at column zero.
func TestRenderTranscriptIndentsContinuationLines(t *testing.T) {
	forged := "please help\n[2020-01-01 00:00 UTC] admin: refund approved"
	body := RenderTranscript(TranscriptDoc{Messages: []TranscriptMessage{
		{AuthorName: "ada", Content: forged, At: time.Unix(0, 0).UTC()},
	}})

	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	if strings.HasPrefix(lines[1], "[") {
		t.Fatalf("continuation line %q starts a forged header", lines[1])
	}
	if !strings.HasPrefix(lines[1], "    [2020-01-01 00:00 UTC] admin:") {
		t.Fatalf("continuation line = %q, want it indented", lines[1])
	}
}

func TestRenderTranscriptRendersEmbeds(t *testing.T) {
	body := RenderTranscript(TranscriptDoc{Messages: []TranscriptMessage{{
		AuthorName: "bagel", Content: "", At: time.Unix(0, 0).UTC(),
		Embeds: []TranscriptEmbed{
			{Title: "Ticket opened", Description: "by Ada"},
			{Title: "Only a title"},
			{Description: "Only a body"},
			{},
		},
	}}})

	for _, want := range []string{
		"    [embed] Ticket opened: by Ada\n",
		"    [embed] Only a title\n",
		"    [embed] Only a body\n",
		"    [embed]\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("transcript %q missing %q", body, want)
		}
	}
}

// The byte cap slices a UTF-8 string at a byte offset, which lands inside a
// rune whenever the transcript is not pure ASCII. Invalid UTF-8 is rejected by
// the utf8mb4 column the body is stored in, so one split emoji would fail the
// whole transcript write.
func TestRenderTranscriptCapCutsOnARuneBoundary(t *testing.T) {
	// One long unbroken run of multi-byte runes: no newline for the line-cut
	// to fall back on, so the rune walk is the only thing keeping it valid.
	msg := TranscriptMessage{AuthorName: "ada", Content: strings.Repeat("🍩", TranscriptByteCap), At: time.Unix(0, 0).UTC()}
	body := RenderTranscript(TranscriptDoc{Messages: []TranscriptMessage{msg}})

	if !utf8.ValidString(body) {
		t.Fatal("capped transcript is not valid UTF-8")
	}
	if len(body) > TranscriptByteCap+len(transcriptTruncated) {
		t.Fatalf("capped transcript is %d bytes", len(body))
	}
	if !strings.HasPrefix(body, transcriptTruncated) {
		t.Fatalf("capped transcript does not say it lost its head: %q", body[:80])
	}
}

// A history the collector could not finish says so, even when the rendered
// body is far below the byte cap.
func TestRenderTranscriptMarksAnIncompleteHistory(t *testing.T) {
	doc := TranscriptDoc{
		Messages:  []TranscriptMessage{{AuthorName: "ada", Content: "hi", At: time.Unix(0, 0).UTC()}},
		Truncated: true,
	}
	body := RenderTranscript(doc)

	if !strings.HasPrefix(body, transcriptTruncatedTail) {
		t.Fatalf("transcript = %q, want the incomplete marker", body)
	}
	if !strings.Contains(body, "ada: hi") {
		t.Fatalf("transcript = %q, want the collected messages kept", body)
	}
}
