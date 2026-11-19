// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"
)

type artistLevel uint8

const (
	artistNone artistLevel = iota
	artistPartial
	artistFull
)

type artistMatch struct {
	level       artistLevel
	edits       int
	index       int
	combination bool
	partial     matchPhrase
	corrections []artistEdit
}

type artistEdit struct{ query, credit matchPhrase }

const (
	noCredit          = 1 << 20
	partialCreditCost = 1 << 10
)

var artistSeparators = map[string]bool{
	"and": true, "x": true, "vs": true,
}

func creditWords(credits []string) []matchPhrase {
	words := make([]matchPhrase, 0, len(credits))
	for _, credit := range credits {
		words = append(words, matchWords(credit))
	}
	return words
}

type creditMemo struct {
	matches map[creditIdentity]artistMatch
	queries map[phraseSlice]artistQuery
}

type phraseSlice struct {
	first *string
	size  int
}

func (m *creditMemo) query(words matchPhrase) artistQuery {
	key := phraseSlice{&words[0], len(words)}
	if span, ok := m.queries[key]; ok {
		return span
	}
	span := newArtistQuery(words)
	m.queries[key] = span
	return span
}

type creditIdentity struct{ span, credit string }

type artistQuery struct {
	words matchPhrase
	text  string
	ends  []int
}

func newArtistQuery(words matchPhrase) artistQuery {
	query := artistQuery{words: words, text: words.text(), ends: make([]int, len(words))}
	end := -1
	for i, word := range words {
		end += len(word) + 1
		query.ends[i] = end
	}
	return query
}

func (q artistQuery) span(at wordRange) string {
	start := 0
	if at.start > 0 {
		start = q.ends[at.start-1] + 1
	}
	return q.text[start:q.ends[at.end-1]]
}

type creditMatcher struct {
	memo    *creditMemo
	credits []matchPhrase
	names   []string
	maxSpan int
}

func (memo *creditMemo) match(query matchPhrase, credits []matchPhrase) artistMatch {
	query = query.withoutArticle()
	if len(query) == 0 || len(query) > creditWordRoom(credits) {
		return artistMatch{}
	}
	m := creditMatcher{memo: memo, credits: credits, names: make([]string, len(credits))}
	for i, credit := range credits {
		m.names[i] = credit.text()
		m.maxSpan = max(m.maxSpan, len(m.names[i]))
	}
	span := memo.query(query)
	best := m.single(span, wordRange{0, len(query)})
	if best.level == artistFull || len(credits) < 2 {
		return best
	}
	if groups := artistGroups(query); len(groups) > 1 {
		best = best.prefer(m.assign(groups))
	}
	if best.exact() {
		return best
	}
	return best.prefer(m.implicit(span))
}

func creditWordRoom(credits []matchPhrase) int {
	room := 2 * len(credits)
	for _, credit := range credits {
		room += len(credit)
	}
	return room
}

func (a artistMatch) prefer(b artistMatch) artistMatch {
	if b.compare(a) > 0 {
		return b
	}
	return a
}

func (a artistMatch) compare(b artistMatch) int {
	if order := cmp.Compare(a.level, b.level); order != 0 {
		return order
	}
	return cmp.Compare(b.edits, a.edits)
}

func (m creditMatcher) single(query artistQuery, span wordRange) artistMatch {
	best := artistMatch{index: -1}
	words, key := span.words(query.words), query.span(span)
	for i := range m.credits {
		current := m.compare(words, key, i)
		current.index = i
		best = best.prefer(current)
	}
	return best
}

func (m creditMatcher) compare(words matchPhrase, span string, index int) artistMatch {
	credit := m.credits[index]
	if len(words.withoutArticle()) > len(m.names[index]) {
		return artistMatch{}
	}
	key := creditIdentity{span, m.names[index]}
	if match, ok := m.memo.matches[key]; ok {
		return match
	}
	match := matchArtist(words, credit)
	m.memo.matches[key] = match
	return match
}

func matchArtist(query, credit matchPhrase) artistMatch {
	if edits, ok := artistIdentity(query, credit); ok {
		match := artistMatch{level: artistFull}
		match.addEdit(artistEdit{query, credit}, edits)
		return match
	}
	if partialCredit(query, credit) {
		return artistMatch{level: artistPartial, partial: query}
	}
	return artistMatch{}
}

func (m *artistMatch) addEdit(edit artistEdit, cost int) {
	m.edits += cost
	if cost > 0 {
		m.corrections = append(m.corrections, edit)
	}
}

func artistIdentity(query, credit matchPhrase) (int, bool) {
	edits, ok := phraseIdentity(query, credit)
	if !ok || !needsMononymCheck(creditPair{query, credit}, edits) {
		return edits, ok
	}
	return edits, plausibleMononym(query[0], credit[0])
}

func (p runePair) plausibleSubstitution() bool {
	return p.hasTransposition() || p.keyboardNeighbor()
}

func (p runePair) keyboardNeighbor() bool {
	for i, r := range p.a {
		if r == p.b[i] {
			continue
		}
		return adjacentKeys(runePair{p.a[i : i+1], p.b[i : i+1]})
	}
	return false
}

func adjacentKeys(pair runePair) bool {
	for _, row := range []string{"qwertyuiop", "asdfghjkl", "zxcvbnm"} {
		a, b := strings.IndexRune(row, pair.a[0]), strings.IndexRune(row, pair.b[0])
		if min(a, b) >= 0 && max(a, b)-min(a, b) == 1 {
			return true
		}
	}
	return false
}

type creditPair struct{ query, credit matchPhrase }

func needsMononymCheck(pair creditPair, edits int) bool {
	return edits > 0 && len(pair.query) == 1 && len(pair.credit) == 1
}

func plausibleMononym(query, credit string) bool {
	pair := runePair{[]rune(query), []rune(credit)}
	if len(pair.a) >= 8 || len(pair.b) >= 8 {
		return true
	}
	if len(pair.a) == len(pair.b) {
		return pair.plausibleSubstitution()
	}
	if strings.HasSuffix(credit, query+"s") {
		return false
	}
	if len(pair.a) < len(pair.b) {
		return slices.Equal(pair.a[:3], pair.b[:3])
	}
	return !strings.HasPrefix(query, credit)
}

func (p runePair) hasTransposition() bool {
	for i := 0; i+1 < len(p.a); i++ {
		if p.a[i] != p.b[i] && p.transposable(editCell{row: i + 1, column: i + 1}) {
			return true
		}
	}
	return false
}

func artistSeparator(word string) bool {
	return artistSeparators[word] || hasRecordingRole(matchPhrase{word}, featureCredit)
}

func artistGroups(query matchPhrase) []matchPhrase {
	var groups []matchPhrase
	start := 0
	for i, word := range query {
		if !artistSeparator(word) {
			continue
		}
		if i == start {
			return nil
		}
		groups = append(groups, query[start:i])
		start = i + 1
	}
	if start == len(query) {
		return nil
	}
	return append(groups, query[start:])
}

func partialCredit(query, credit matchPhrase) bool {
	query = query.withoutArticle()
	if len(query) == 0 || len(query) >= len(credit) {
		return false
	}
	if hasRecordingRole(credit, derivativeCredit) {
		return false
	}
	return slices.Equal(query, credit[:len(query)]) ||
		slices.Equal(query, credit[len(credit)-len(query):]) || acronymCredit(query, credit)
}

func acronymCredit(query, credit matchPhrase) bool {
	if len(query) != 1 || len(credit) < 3 {
		return false
	}
	letters := []rune(query[0])
	if len(letters) != len(credit) {
		return false
	}
	for i, word := range credit {
		initial, _ := utf8.DecodeRuneInString(word)
		if letters[i] != initial {
			return false
		}
	}
	return true
}

func (m creditMatcher) implicit(query artistQuery) artistMatch {
	ends := make([]int, len(query.words))
	if !m.implicitPath(query, 0, ends) || ends[0] == len(query.words) {
		return artistMatch{}
	}
	var groups []matchPhrase
	for start := 0; start < len(query.words); start = ends[start] {
		groups = append(groups, query.words[start:ends[start]])
	}
	return m.assign(groups)
}

func (m creditMatcher) implicitPath(query artistQuery, start int, ends []int) bool {
	if start == len(query.words) {
		return true
	}
	if ends[start] != 0 {
		return ends[start] > 0
	}
	ends[start] = m.implicitEnd(query, start, ends)
	return ends[start] > 0
}

func (m creditMatcher) implicitEnd(query artistQuery, start int, ends []int) int {
	last := min(len(query.words), start+m.maxSpan+1)
	for end := last; end > start; end-- {
		if m.single(query, wordRange{start, end}).level == artistNone {
			continue
		}
		if m.implicitPath(query, end, ends) {
			return end
		}
	}
	return -1
}

func exactCredit(query, credit matchPhrase) artistLevel {
	if sameCompact(query.withoutArticle(), credit.withoutArticle()) {
		return artistFull
	}
	if partialCredit(query, credit) {
		return artistPartial
	}
	return artistNone
}

func hasExactCredit(query matchPhrase, credits []matchPhrase) bool {
	return slices.ContainsFunc(credits, func(credit matchPhrase) bool {
		return exactCredit(query, credit) == artistFull
	})
}

func (m creditMatcher) assign(groups []matchPhrase) artistMatch {
	if len(groups) > len(m.credits) {
		return artistMatch{}
	}
	costs := make(creditCosts, len(groups))
	for i, group := range groups {
		costs[i] = m.costs(group)
	}
	return costs.minimum(groups, m.credits)
}

func (m creditMatcher) costs(group matchPhrase) []int {
	cost := make([]int, len(m.credits))
	text := group.text()
	for i := range m.credits {
		switch match := m.compare(group, text, i); match.level {
		case artistFull:
			cost[i] = match.edits
		case artistPartial:
			cost[i] = partialCreditCost
		default:
			cost[i] = noCredit
		}
	}
	return cost
}
