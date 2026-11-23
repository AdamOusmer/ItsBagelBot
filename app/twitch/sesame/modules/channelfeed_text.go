// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"slices"

	"ItsBagelBot/internal/domain/channelevents"
)

type textSpan struct {
	id         string
	begin, end int
}

// inclusiveSpans converts EventSub emote offsets, whose end is the last rune, to half-open spans.
func inclusiveSpans(emotes []feedEmote) []textSpan {
	spans := make([]textSpan, len(emotes))
	for i, e := range emotes {
		spans[i] = textSpan{id: e.ID, begin: e.Begin, end: e.End + 1}
	}
	return spans
}

func (s textSpan) fits(cursor, length int) bool {
	return s.id != "" && s.begin >= cursor && s.begin < s.end && s.end <= length
}

// fragments splits text into text and emote parts; spans that overlap or leave the text are dropped.
func fragments(text string, spans []textSpan) []channelevents.Fragment {
	if len(spans) == 0 {
		return nil
	}
	runes := []rune(text)
	slices.SortFunc(spans, func(a, b textSpan) int { return a.begin - b.begin })
	var parts []channelevents.Fragment
	cursor := 0
	for _, span := range spans {
		if !span.fits(cursor, len(runes)) {
			continue
		}
		if span.begin > cursor {
			parts = append(parts, channelevents.Fragment{Type: "text", Text: string(runes[cursor:span.begin])})
		}
		parts = append(parts, channelevents.Fragment{Type: "emote", ID: span.id, Text: string(runes[span.begin:span.end])})
		cursor = span.end
	}
	if cursor < len(runes) {
		parts = append(parts, channelevents.Fragment{Type: "text", Text: string(runes[cursor:])})
	}
	return parts
}
