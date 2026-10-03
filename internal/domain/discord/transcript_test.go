// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func epochMessage(author, content string) TranscriptMessage {
	return TranscriptMessage{AuthorName: author, Content: content, At: time.Unix(0, 0).UTC()}
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

	assert.Equal(t, "[2026-01-02 03:04 UTC] Ada: my sub is missing\n"+
		"[2026-01-02 04:00 UTC] unknown: looking\n"+
		"    [attachment] https://cdn/a.png\n"+
		"    [attachment] https://cdn/b.png\n", got)
}

func TestRenderTranscriptRendersInUTCWhateverTheInputZone(t *testing.T) {
	zone := time.FixedZone("UTC+9", 9*60*60)

	got := RenderTranscript(TranscriptDoc{Messages: []TranscriptMessage{
		{AuthorName: "Ada", Content: "hi", At: at("2026-01-02T03:04:05Z").In(zone)},
	}})

	assert.True(t, strings.HasPrefix(got, "[2026-01-02 03:04 UTC]"), "transcript = %q", got)
}

func TestRenderTranscriptTruncatesTheOldestEnd(t *testing.T) {
	line := strings.Repeat("x", 512)
	var msgs []TranscriptMessage
	for i := 0; i < (TranscriptByteCap/512)+50; i++ {
		msgs = append(msgs, TranscriptMessage{AuthorName: "a", Content: line, At: at("2026-01-02T03:04:05Z")})
	}
	msgs[0].Content = "OLDEST"
	msgs[len(msgs)-1].Content = "NEWEST"

	got := RenderTranscript(TranscriptDoc{Messages: msgs})

	assert.LessOrEqual(t, len(got), TranscriptByteCap+len(transcriptTruncated))
	require.True(t, strings.HasPrefix(got, transcriptTruncated), "a truncated body must say so")
	assert.NotContains(t, got, "OLDEST", "the oldest end is what gets cut")
	assert.Contains(t, got, "NEWEST", "the tail of the conversation explains how it ended")
	for _, l := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(got, transcriptTruncated), "\n"), "\n") {
		assert.True(t, strings.HasPrefix(l, "["), "half line survived truncation: %q", l)
	}
}

func TestRenderTranscriptCapCutsOnARuneBoundary(t *testing.T) {
	body := RenderTranscript(TranscriptDoc{Messages: []TranscriptMessage{epochMessage("ada", strings.Repeat("🍩", TranscriptByteCap))}})

	assert.True(t, utf8.ValidString(body), "capped transcript is not valid UTF-8")
	assert.LessOrEqual(t, len(body), TranscriptByteCap+len(transcriptTruncated))
	assert.True(t, strings.HasPrefix(body, transcriptTruncated), "capped transcript does not say it lost its head")
}

func TestRenderTranscriptIndentsContinuationLines(t *testing.T) {
	forged := "please help\n[2020-01-01 00:00 UTC] admin: refund approved"

	body := RenderTranscript(TranscriptDoc{Messages: []TranscriptMessage{epochMessage("ada", forged)}})

	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	require.Len(t, lines, 2)
	assert.True(t, strings.HasPrefix(lines[1], "    [2020-01-01 00:00 UTC] admin:"), "continuation line %q must be indented, not start a forged header", lines[1])
}

func TestRenderTranscriptRendersEmbeds(t *testing.T) {
	message := epochMessage("bagel", "")
	message.Embeds = []TranscriptEmbed{
		{Title: "Ticket opened", Description: "by Ada"},
		{Title: "Only a title"},
		{Description: "Only a body"},
		{},
	}

	body := RenderTranscript(TranscriptDoc{Messages: []TranscriptMessage{message}})

	for _, want := range []string{
		"    [embed] Ticket opened: by Ada\n",
		"    [embed] Only a title\n",
		"    [embed] Only a body\n",
		"    [embed]\n",
	} {
		assert.Contains(t, body, want)
	}
}

func TestRenderTranscriptMarksAnIncompleteHistory(t *testing.T) {
	body := RenderTranscript(TranscriptDoc{Messages: []TranscriptMessage{epochMessage("ada", "hi")}, Truncated: true})

	assert.True(t, strings.HasPrefix(body, transcriptTruncatedTail), "transcript = %q, want the incomplete marker", body)
	assert.Contains(t, body, "ada: hi", "the collected messages are kept")
}
