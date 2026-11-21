// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxTags           = 4
	maxPauseTags      = 3
	maxLongPauses     = 1
	maxPunctuationRun = 3
)

type speechTag uint8

const (
	plainText speechTag = iota
	expressiveTag
	shortPause
	longPause
)

var speechTags = map[string]speechTag{
	"short pause": shortPause,
	"long pause":  longPause,
	"laugh":       expressiveTag,
	"cheer":       expressiveTag,
	"sigh":        expressiveTag,
	"whispers":    expressiveTag,
	"breath":      expressiveTag,
}

var (
	angleBrackets   = regexp.MustCompile(`<[^<>]*>|[<>]`)
	punctuationRuns = regexp.MustCompile(`[-.…_~,]+`)
)

type speechPart struct {
	text string
	tag  speechTag
}

type speechShaper struct {
	out     strings.Builder
	lastTag string
	kept    [longPause + 1]int
	total   int
}

func Shape(text string) string {
	parts := splitSpeech(text)
	first, last := spokenBounds(parts)
	if first < 0 {
		return ""
	}

	var shaper speechShaper
	for i, part := range parts {
		shaper.writePart(part, first < i && i < last)
	}
	normalized := punctuationRuns.ReplaceAllStringFunc(shaper.out.String(), shortenPunctuation)
	return strings.Join(strings.Fields(normalized), " ")
}

func splitSpeech(text string) []speechPart {
	matches := angleBrackets.FindAllStringIndex(text, -1)
	parts := make([]speechPart, 0, 2*len(matches)+1)
	start := 0
	for _, match := range matches {
		parts = append(parts, speechPart{text: text[start:match[0]]}, anglePart(text[match[0]:match[1]]))
		start = match[1]
	}
	return append(parts, speechPart{text: text[start:]})
}

func anglePart(token string) speechPart {
	inner := strings.Trim(token, "<>")
	name := strings.ToLower(strings.Join(strings.Fields(inner), " "))
	if tag, ok := speechTags[name]; ok {
		return speechPart{text: "<" + name + ">", tag: tag}
	}
	return speechPart{text: " " + inner + " "}
}

func spokenBounds(parts []speechPart) (first, last int) {
	first = -1
	for i, part := range parts {
		if !part.hasSpokenText() {
			continue
		}
		if first == -1 {
			first = i
		}
		last = i
	}
	return first, last
}

func (p speechPart) hasSpokenText() bool {
	return p.tag == plainText && strings.ContainsFunc(p.text, speechLetterOrDigit)
}

func speechLetterOrDigit(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (s *speechShaper) writePart(part speechPart, betweenWords bool) {
	switch {
	case part.tag == plainText:
		s.writeText(part)
	case part.text == s.lastTag:
		return
	case s.allowsTag(part.tag, betweenWords):
		s.out.WriteString(part.text)
		s.lastTag = part.text
		s.kept[part.tag]++
		s.total++
	default:
		s.out.WriteByte(' ')
	}
}

func (s *speechShaper) writeText(part speechPart) {
	s.out.WriteString(part.text)
	if strings.ContainsFunc(part.text, separatesTags) {
		s.lastTag = ""
	}
}

func separatesTags(r rune) bool {
	return !unicode.IsSpace(r) && !unicode.IsPunct(r) && r != '~'
}

func (s *speechShaper) allowsTag(tag speechTag, betweenWords bool) bool {
	switch {
	case s.total >= maxTags:
		return false
	case tag == expressiveTag:
		return true
	case !betweenWords, s.kept[shortPause]+s.kept[longPause] >= maxPauseTags:
		return false
	default:
		return tag != longPause || s.kept[longPause] < maxLongPauses
	}
}

func shortenPunctuation(run string) string {
	switch {
	case utf8.RuneCountInString(run) <= maxPunctuationRun:
		return run
	case strings.Trim(run, "-") == "":
		return "--"
	default:
		return "..."
	}
}
