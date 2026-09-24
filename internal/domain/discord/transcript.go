// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strings"
	"time"
	"unicode/utf8"
)

const TranscriptMessageCap = 2000

const TranscriptByteCap = 2 << 20

const transcriptTruncated = "[earlier messages omitted: transcript truncated]\n"

const transcriptTruncatedTail = "[transcript incomplete: the oldest messages could not be collected]\n"

const transcriptIndent = "    "

type TranscriptEmbed struct {
	Title       string
	Description string
}

type TranscriptMessage struct {
	AuthorName  string
	Content     string
	At          time.Time
	Attachments []string
	Embeds      []TranscriptEmbed
}

type TranscriptDoc struct {
	ChannelName string
	Messages    []TranscriptMessage
	Truncated   bool
}

func RenderTranscript(doc TranscriptDoc) string {
	var b strings.Builder
	for _, m := range doc.Messages {
		b.WriteString(transcriptLine(m))
	}
	return capBody(b.String(), doc.Truncated)
}

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

func indentContinuation(s string) string {
	return strings.ReplaceAll(s, "\n", "\n"+transcriptIndent)
}

func capBody(body string, incomplete bool) string {
	if len(body) > TranscriptByteCap {
		return transcriptTruncated + trimToLine(body[len(body)-TranscriptByteCap:])
	}
	if incomplete {
		return transcriptTruncatedTail + body
	}
	return body
}

func trimToLine(tail string) string {
	tail = alignRune(tail)
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i+1 < len(tail) {
		return tail[i+1:]
	}
	return tail
}

func alignRune(s string) string {
	for i := 0; i < len(s) && i < utf8.UTFMax; i++ {
		if utf8.RuneStart(s[i]) {
			return s[i:]
		}
	}
	return s
}
