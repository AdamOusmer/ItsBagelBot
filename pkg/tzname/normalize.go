// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// maxQueryRunes caps a normalized query so a pasted paragraph (or a chat
// client that lets someone type past 64 chars) can't turn every lookup below
// into a scan of a viewer-controlled amount of text.
const maxQueryRunes = 64

// trailingPunct is stripped from the end of a query after trimming: the
// sentence-ending marks a Twitch chat message is likely to trail off with
// ("what time is it in tokyo?", "london!", "paris.", "ottawa,") but that
// never appear inside a place name written in Latin script.
const trailingPunct = "?!.,"

// Normalize is the cleaned form every lookup in Resolve runs on; exported so
// callers can echo it back (e.g. "didn't recognize <normalized>, try a
// city or UTC offset"). Underscores fold to spaces first so "new_york" and
// "new york" become the same query; a leading @/# is stripped because
// viewers mention places the same way they mention people; trailing
// punctuation and internal whitespace runs are cleaned up; the result is
// capped, case-folded, and accent-folded so "São Paulo" and "sao paulo"
// hit the same table entry.
func Normalize(query string) string {
	s := strings.ReplaceAll(query, "_", " ")
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "@#")
	s = strings.TrimRight(s, trailingPunct)
	s = strings.Join(strings.Fields(s), " ")
	s = capRunes(s, maxQueryRunes)
	return foldAccents(strings.ToLower(s))
}

// capRunes truncates by rune count, not byte count, so multi-byte UTF-8
// (accents, CJK) can't be split mid-rune at the boundary.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// foldAccents decomposes s (Unicode NFD) and drops the combining marks that
// decomposition leaves behind, collapsing an accented Latin letter to its
// plain base ("é"->"e", "ü"->"u", "ç"->"c", "ñ"->"n"). x/text/unicode/norm is
// already a repo dependency (internal/domain/validate, internal/moderation
// both use it for NFKC), so this reuses it rather than hand-rolling a rune
// table; unlike those two callers this package only needs NFD+strip, not
// NFKC's fullwidth/compatibility folding, since chat commands arrive as
// plain typed text, not copy-pasted glyph tricks.
func foldAccents(s string) string {
	decomposed := norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
