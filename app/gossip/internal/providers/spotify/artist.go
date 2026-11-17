// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"math"
	"slices"
	"unicode/utf8"
)

type artistLevel uint8

const (
	artistNone artistLevel = iota
	artistPartial
	artistFull
)

var artistSeparators = map[string]bool{
	"and": true, "x": true, "feat": true, "ft": true, "featuring": true, "with": true, "vs": true,
}

var derivativeCredits = map[string]bool{
	"tribute": true, "karaoke": true, "cover": true, "covers": true, "made": true,
	"famous": true, "originally": true, "performed": true, "style": true, "lullaby": true,
	"lullabies": true, "piano": true, "orchestra": true, "quartet": true, "ensemble": true,
	"kids": true, "players": true, "soundalike": true,
}

func creditWords(credits []string) []matchPhrase {
	words := make([]matchPhrase, 0, len(credits))
	for _, credit := range credits {
		words = append(words, matchWords(credit))
	}
	return words
}

func creditEvidence(query matchPhrase, credits []matchPhrase) (artistLevel, int) {
	query = query.withoutArticle()
	if len(query) == 0 {
		return artistNone, 0
	}
	if edits, ok := singleCredit(query, credits); ok {
		return artistFull, edits
	}
	if edits, ok := distinctCredits(artistGroups(query), credits); ok {
		return artistFull, edits
	}
	return implicitCredits(query, credits)
}

func singleCredit(query matchPhrase, credits []matchPhrase) (int, bool) {
	best := math.MaxInt
	for _, credit := range credits {
		edits, ok := phraseIdentity(query, credit)
		if ok {
			best = min(best, edits)
		}
	}
	return best, best != math.MaxInt
}

func artistGroups(query matchPhrase) []matchPhrase {
	var groups []matchPhrase
	start := 0
	for i, word := range query {
		if !artistSeparators[word] {
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

func distinctCredits(groups, credits []matchPhrase) (int, bool) {
	if len(groups) < 2 || len(groups) > len(credits) {
		return 0, false
	}
	used := make([]bool, len(credits))
	return assignCredits(groups, credits, used)
}

func assignCredits(groups, credits []matchPhrase, used []bool) (int, bool) {
	if len(groups) == 0 {
		return 0, true
	}
	best := math.MaxInt
	for i, credit := range credits {
		edits, ok := phraseIdentity(groups[0], credit)
		if used[i] || !ok {
			continue
		}
		used[i] = true
		rest, found := assignCredits(groups[1:], credits, used)
		used[i] = false
		if found {
			best = min(best, edits+rest)
		}
	}
	return best, best != math.MaxInt
}

func partialCredit(query, credit matchPhrase) bool {
	query = query.withoutArticle()
	if !validPartialLength(query, credit) || slices.ContainsFunc(query, shortArtistWord) {
		return false
	}
	cut := len(credit) - len(query)
	return creditEdge(query, credit[:len(query)], credit[len(query):]) || creditEdge(query, credit[cut:], credit[:cut])
}

func shortArtistWord(word string) bool {
	return utf8.RuneCountInString(word) < 4
}

func validPartialLength(query, credit matchPhrase) bool {
	return len(query) > 0 && len(query) < len(credit)
}

func creditEdge(query, edge, extra matchPhrase) bool {
	return slices.Equal(query, edge) &&
		!slices.ContainsFunc(extra, func(word string) bool { return derivativeCredits[word] })
}

func implicitCredits(query matchPhrase, credits []matchPhrase) (artistLevel, int) {
	if len(credits) < 2 {
		return artistNone, 0
	}
	best := artistNone
	for i := 1; i < len(query); i++ {
		best = max(best, creditPair(query[:i], query[i:], credits))
	}
	return best, 0
}

func creditPair(first, second matchPhrase, credits []matchPhrase) artistLevel {
	best := artistNone
	for i, credit := range credits {
		left := exactCredit(first, credit)
		if left == artistNone {
			continue
		}
		best = max(best, partnerCredit(second, credits, i, left))
	}
	return best
}

func partnerCredit(query matchPhrase, credits []matchPhrase, skip int, left artistLevel) artistLevel {
	best := artistNone
	for i, credit := range credits {
		right := exactCredit(query, credit)
		if i == skip || max(left, right) != artistFull {
			continue
		}
		best = max(best, min(left, right))
	}
	return best
}

func exactCredit(query, credit matchPhrase) artistLevel {
	if edits, ok := phraseIdentity(query, credit); ok && edits == 0 {
		return artistFull
	}
	if partialCredit(query, credit) {
		return artistPartial
	}
	return artistNone
}
