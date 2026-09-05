// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strings"
	"time"
	"unicode/utf8"
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

// transcriptTruncatedTail is the marker appended to a body whose OLDEST
// messages were never collected at all -- the message cap tripped, or a page
// of history failed. Distinct from transcriptTruncated (which the byte cap
// writes) only in wording; both say the same thing to a reader, and keeping
// two constants lets a test tell the two causes apart.
const transcriptTruncatedTail = "[transcript incomplete: the oldest messages could not be collected]\n"

// transcriptIndent prefixes every line of a message that is not its header.
//
// This is a forgery guard, not formatting. A transcript line is
// "[time] name: content", and content is whatever a member typed -- including
// a literal "[2020-01-01 00:00 UTC] admin: approved". Indenting every
// continuation line means a forged line can never begin at column zero, which
// is the only place the renderer ever writes a real header.
const transcriptIndent = "    "

// TranscriptEmbed is the readable slice of an embed on a transcribed message.
// Only the title and the description are rendered: fields, images and footers
// are decoration, and a plain-text transcript that reproduced them would be
// longer than the conversation it is meant to preserve.
type TranscriptEmbed struct {
	Title       string
	Description string
}

// TranscriptMessage is one rendered line's input, in the shape the REST list
// call returns.
type TranscriptMessage struct {
	AuthorName  string
	Content     string
	At          time.Time
	Attachments []string
	Embeds      []TranscriptEmbed
}

// TranscriptDoc is a whole ticket's history, OLDEST first (the REST listing
// returns newest first, so the caller reverses before rendering: a transcript
// read top to bottom is the conversation in the order it happened).
type TranscriptDoc struct {
	ChannelName string
	Messages    []TranscriptMessage
	// Truncated marks a history the collector could not finish: the message
	// cap tripped, or a page failed. The renderer emits a marker for it, so a
	// short transcript is never mistaken for a short conversation.
	Truncated bool
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
	return capBody(b.String(), doc.Truncated)
}

// transcriptLine renders one message as "[YYYY-MM-DD HH:MM UTC] name: content".
// Everything after the header -- the rest of a multi-line body, each embed,
// each attachment -- is indented (see transcriptIndent).
func transcriptLine(m TranscriptMessage) string {
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(m.At.UTC().Format("2006-01-02 15:04"))
	b.WriteString(" UTC] ")
	b.WriteString(orUnknown(m.AuthorName))
	b.WriteString(": ")
	b.WriteString(indentContinuation(m.Content))
	b.WriteString("\n")
	for _, e := range m.Embeds {
		writeIndented(&b, embedLine(e))
	}
	for _, url := range m.Attachments {
		writeIndented(&b, "[attachment] "+url)
	}
	return b.String()
}

// embedLine renders one embed as "[embed] title: description", degrading to
// whichever half exists and to a bare "[embed]" when neither does -- an embed
// with only an image still has to appear, or the transcript reads as if the
// message were empty.
func embedLine(e TranscriptEmbed) string {
	switch {
	case e.Title != "" && e.Description != "":
		return "[embed] " + e.Title + ": " + e.Description
	case e.Title != "":
		return "[embed] " + e.Title
	case e.Description != "":
		return "[embed] " + e.Description
	default:
		return "[embed]"
	}
}

func writeIndented(b *strings.Builder, line string) {
	b.WriteString(transcriptIndent)
	b.WriteString(indentContinuation(line))
	b.WriteString("\n")
}

// indentContinuation pushes every line after the first behind transcriptIndent.
func indentContinuation(s string) string {
	return strings.ReplaceAll(s, "\n", "\n"+transcriptIndent)
}

// capBody trims a body to TranscriptByteCap, keeping the tail, and marks a
// body that lost its head for either reason (the byte cap here, or a collector
// that never had the oldest messages to begin with).
func capBody(body string, incomplete bool) string {
	if len(body) > TranscriptByteCap {
		return transcriptTruncated + trimToLine(body[len(body)-TranscriptByteCap:])
	}
	if incomplete {
		return transcriptTruncatedTail + body
	}
	return body
}

// trimToLine drops the partial head of a byte-sliced tail: first forward to a
// rune boundary, then forward to the next line.
//
// The rune walk is not belt-and-braces. Slicing at a byte offset lands inside a
// multi-byte rune whenever the transcript contains non-ASCII text, and the
// resulting invalid UTF-8 is rejected by MySQL's utf8mb4 column -- the whole
// transcript write fails on one split emoji. Cutting to the line boundary
// afterwards usually subsumes this, but only when the tail contains a newline
// at all.
func trimToLine(tail string) string {
	tail = alignRune(tail)
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i+1 < len(tail) {
		return tail[i+1:]
	}
	return tail
}

// alignRune advances past the continuation bytes of a rune the slice cut in
// half. A UTF-8 rune is at most 4 bytes, so at most 3 bytes are ever skipped.
func alignRune(s string) string {
	for i := 0; i < len(s) && i < utf8.UTFMax; i++ {
		if utf8.RuneStart(s[i]) {
			return s[i:]
		}
	}
	return s
}
