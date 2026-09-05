// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strings"
	"time"
)

// TranscriptMessageCap bounds how many messages one closing ticket transcribes.
//
// 2000 is ~20 paginated REST calls at Discord's 100-per-page maximum, which at
// the shared ~50 req/s token budget is under half a second of the whole fleet's
// send capacity -- acceptable for an operation a guild performs a handful of
// times a day. Uncapped was the alternative and is not safe: a ticket channel
// that a raid filled, or one a streamer used as a general chat for a month,
// would page for minutes and hold a lane worker the entire time. A transcript
// that hits the cap is truncated at the OLDEST end (the newest 2000 messages
// are kept), because the tail of a support conversation is the part that
// explains how it ended.
const TranscriptMessageCap = 2000

// TranscriptByteCap bounds the rendered body.
//
// 2 MiB is the ticket_transcripts.body column's documented ceiling (MySQL
// LONGTEXT holds far more, but the row travels through a NATS reply on read and
// the default max_payload is 8 MiB). 2000 messages of ordinary chat render to
// ~150 KiB, so this only ever trips on pasted logs; the body is truncated at the
// oldest end for the same reason as the message cap, with a marker line so a
// reader is never left guessing whether the top is missing.
const TranscriptByteCap = 2 << 20

// transcriptTruncated is the marker prepended to a body that lost its head.
const transcriptTruncated = "[earlier messages omitted: transcript truncated]\n"

// TranscriptMessage is one rendered line's input, in the shape the REST list
// call returns.
type TranscriptMessage struct {
	AuthorName  string
	Content     string
	At          time.Time
	Attachments []string
}

// TranscriptDoc is a whole ticket's history, OLDEST first (the REST listing
// returns newest first, so the caller reverses before rendering: a transcript
// read top to bottom is the conversation in the order it happened).
type TranscriptDoc struct {
	ChannelName string
	Messages    []TranscriptMessage
}

// RenderTranscript renders a plain-text transcript.
//
// Plain text, not JSON or HTML: the file is attached to a Discord message and
// read in Discord's own preview pane, which renders .txt inline and offers
// anything else only as a download.
func RenderTranscript(doc TranscriptDoc) string {
	var b strings.Builder
	for _, m := range doc.Messages {
		b.WriteString(transcriptLine(m))
	}
	return capBody(b.String())
}

// transcriptLine renders one message as "[YYYY-MM-DD HH:MM UTC] name: content",
// with each attachment on its own following line.
func transcriptLine(m TranscriptMessage) string {
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(m.At.UTC().Format("2006-01-02 15:04"))
	b.WriteString(" UTC] ")
	b.WriteString(orUnknown(m.AuthorName))
	b.WriteString(": ")
	b.WriteString(m.Content)
	b.WriteString("\n")
	for _, url := range m.Attachments {
		b.WriteString("    [attachment] ")
		b.WriteString(url)
		b.WriteString("\n")
	}
	return b.String()
}

// capBody trims a body to TranscriptByteCap, keeping the tail and cutting on a
// line boundary so no half line is stored.
func capBody(body string) string {
	if len(body) <= TranscriptByteCap {
		return body
	}
	tail := body[len(body)-TranscriptByteCap:]
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i+1 < len(tail) {
		tail = tail[i+1:]
	}
	return transcriptTruncated + tail
}
