// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tzname

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const maxQueryRunes = 64

const trailingPunct = "?!.,"

func Normalize(query string) string {
	s := strings.ReplaceAll(query, "_", " ")
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "@#")
	s = strings.TrimRight(s, trailingPunct)
	s = strings.Join(strings.Fields(s), " ")
	s = capRunes(s, maxQueryRunes)
	return foldAccents(strings.ToLower(s))
}

func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

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
