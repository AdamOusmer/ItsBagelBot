// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

func phraseIdentity(query, metadata matchPhrase) (int, bool) {
	query, metadata = query.withoutArticle(), metadata.withoutArticle()
	compact := metadata.compact()
	switch {
	case len(query) == 0 || len(metadata) == 0:
		return 0, false
	case query.compact() == compact:
		return 0, true
	case len(query) != len(metadata):
		return 0, false
	}
	return alignedEdits(query, metadata, 1+utf8.RuneCountInString(compact)/8)
}

func alignedEdits(query, metadata matchPhrase, limit int) (int, bool) {
	edits := 0
	for i, word := range metadata {
		distance, ok := wordIdentity(wordPair{query[i], word})
		edits += distance
		if !ok || edits > limit {
			return 0, false
		}
	}
	return edits, true
}

type wordPair struct{ query, metadata string }

func wordIdentity(pair wordPair) (int, bool) {
	if pair.query == pair.metadata {
		return 0, true
	}
	runes := runePair{[]rune(pair.query), []rune(pair.metadata)}
	limit := wordEditBudget(runes.b)
	if limit == 0 || !runes.allowsEdits() {
		return 0, false
	}
	edits := osaDistance(runes.a, runes.b, limit)
	return edits, edits <= limit
}

func wordEditBudget(word []rune) int {
	switch {
	case len(word) <= 3 || protectedWord(word):
		return 0
	case len(word) < 10:
		return 1
	default:
		return 2
	}
}

func (p runePair) allowsEdits() bool {
	return !protectedWord(p.a) && p.allowedStart()
}

func (p runePair) allowedStart() bool {
	if len(p.b) >= 6 {
		return true
	}
	return len(p.a) <= len(p.b) && (p.a[0] == p.b[0] || p.startsWithSwap())
}

func (p runePair) startsWithSwap() bool {
	return len(p.a) == len(p.b) && p.a[0] == p.b[1] && p.a[1] == p.b[0]
}

var romanNumeral = regexp.MustCompile(`^m{0,3}(cm|cd|d?c{0,3})(xc|xl|l?x{0,3})(ix|iv|v?i{0,3})$`)

func protectedWord(word []rune) bool {
	text := string(word)
	return numberTokens[text] || recordingMarkers[text] || protectedSpelling(text)
}

func protectedSpelling(word string) bool {
	return romanNumeral.MatchString(word) || strings.ContainsFunc(word, protectedRune)
}

func protectedRune(r rune) bool {
	return unicode.In(r, unicode.N, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana) ||
		strings.ContainsRune("ー・", r)
}

func osaDistance(a, b []rune, limit int) int {
	if max(len(a)-len(b), len(b)-len(a)) > limit {
		return limit + 1
	}
	rows := osaRows{
		older: make([]int, len(b)+1), previous: make([]int, len(b)+1), current: make([]int, len(b)+1),
	}
	for j := range rows.previous {
		rows.previous[j] = j
	}
	for i := range a {
		if rows.advance(runePair{a, b}, i) > limit {
			return limit + 1
		}
	}
	return min(rows.previous[len(b)], limit+1)
}

type runePair struct{ a, b []rune }
type editCell struct{ row, column int }
type osaRows struct{ older, previous, current []int }

func (r *osaRows) advance(pair runePair, i int) int {
	r.current[0] = i + 1
	best := r.current[0]
	for j := range pair.b {
		cell := editCell{row: i, column: j}
		distance := min(r.previous[j+1]+1, r.current[j]+1, r.previous[j]+pair.substitution(cell))
		if pair.transposable(cell) {
			distance = min(distance, r.older[j-1]+1)
		}
		r.current[j+1] = distance
		best = min(best, distance)
	}
	r.older, r.previous, r.current = r.previous, r.current, r.older
	return best
}

func (p runePair) substitution(cell editCell) int {
	if p.a[cell.row] == p.b[cell.column] {
		return 0
	}
	return 1
}

func (p runePair) transposable(cell editCell) bool {
	i, j := cell.row, cell.column
	return min(i, j) > 0 && p.a[i] == p.b[j-1] && p.a[i-1] == p.b[j]
}
