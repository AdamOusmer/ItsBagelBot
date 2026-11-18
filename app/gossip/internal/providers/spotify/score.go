// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"cmp"
	"slices"
)

type searchFields struct{ song, artist matchPhrase }

var artistStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "i": true, "it": true,
	"by": true, "of": true, "to": true, "in": true, "and": true,
}

func (v queryVariant) interpretations() []searchFields {
	var fields []searchFields
	if v.titleOnly {
		fields = append(fields, searchFields{song: v.words})
	}
	positions := versionPositions(v.words)
	for _, split := range wordSplits(v.words) {
		field := split.fields(v.words)
		fields = append(fields, field)
		if shifted, ok := split.shiftVersion(v.words, positions); ok {
			fields = append(fields, shifted)
		}
	}
	return fields
}

type fieldSplit struct{ song, artist wordRange }

func (s fieldSplit) fields(words matchPhrase) searchFields {
	return searchFields{song: s.song.words(words), artist: s.artist.words(words)}
}

func (s fieldSplit) shiftVersion(words matchPhrase, positions []int) (searchFields, bool) {
	for _, at := range positions {
		if at <= s.artist.start || at >= s.artist.end {
			continue
		}
		song := slices.Concat(s.song.words(words), words[at:s.artist.end])
		return searchFields{song: song, artist: words[s.artist.start:at]}, true
	}
	return searchFields{}, false
}

func wordSplits(words matchPhrase) []fieldSplit {
	var out []fieldSplit
	for i := len(words) - 1; i >= 1; i-- {
		left, right := wordRange{0, i}, wordRange{i, len(words)}
		out = append(out, fieldSplit{left, right}, fieldSplit{right, left})
	}
	return slices.DeleteFunc(out, func(split fieldSplit) bool {
		artist := split.artist.words(words).withoutArticle()
		return len(artist) == 1 && artistStopwords[artist[0]]
	})
}

type matchQuality uint8

const (
	noMatch matchQuality = iota
	creditOnly
	titlePrefix
	titleOnly
	versionIntent
	blockedRecording
	fallbackMatch
	partialMatch
	fuzzyMatch
	exactMatch
)

type matchScore struct {
	quality matchQuality
	title   titleResult
	artist  artistMatch
	fields  searchFields
}

func (s matchScore) compare(other matchScore) int {
	if order := cmp.Compare(s.quality, other.quality); order != 0 {
		return order
	}
	return cmp.Compare(other.edits(), s.edits())
}

func (s matchScore) edits() int {
	return s.title.edits + s.artist.edits
}

type windowMatch struct {
	track catalogTrack
	score matchScore
}

func (m windowMatch) credit() matchPhrase {
	if m.score.artist.index < 0 || m.score.artist.level == artistNone {
		return m.score.fields.artist
	}
	return m.track.credits[m.score.artist.index]
}

func (m windowMatch) sameSong(other windowMatch) bool {
	if m.track.isrc != "" && m.track.isrc == other.track.isrc {
		return true
	}
	return m.sameTitle(other.track) && sameCompact(m.track.primaryCredit(), other.track.primaryCredit())
}

func (m windowMatch) sameTitle(track catalogTrack) bool {
	return m.track.title.part == track.title.part && sameCompact(m.track.title.base, track.title.base)
}

type rankedWindow struct {
	matches   []windowMatch
	ambiguous bool
}

func (w rankedWindow) top() windowMatch {
	if len(w.matches) == 0 {
		return windowMatch{}
	}
	return w.matches[0]
}

func (scorer windowScorer) rank(window []catalogTrack, fields []searchFields) rankedWindow {
	matches := make([]windowMatch, len(window))
	for _, interpretation := range fields {
		scorer.rankInterpretation(matches, window, interpretation)
	}
	matches = slices.DeleteFunc(matches, func(m windowMatch) bool { return m.score.quality == noMatch })
	slices.SortStableFunc(matches, compareWindowMatches)
	matches = preferAlbum(matches)
	preferUnarranged(matches)
	return rankedWindow{matches: matches, ambiguous: ambiguousTop(matches) || competingPartial(matches, window)}
}

type windowScorer struct {
	credits *creditMemo
	parts   []catalogTrack
}

func (scorer windowScorer) rankInterpretation(matches []windowMatch, window []catalogTrack, fields searchFields) {
	for i, track := range window {
		score := scorer.score(track, fields)
		if score.eligible(window) && score.compare(matches[i].score) > 0 {
			matches[i] = windowMatch{track, score}
		}
	}
}

func (scorer windowScorer) score(track catalogTrack, fields searchFields) matchScore {
	score := track.score(fields, scorer.credits)
	if score.quality == fallbackMatch && scorer.competingPart(track, score.title) {
		score.quality = blockedRecording
	}
	return score
}

func (scorer windowScorer) competingPart(track catalogTrack, title titleResult) bool {
	if title.version || !track.title.hasPart() {
		return false
	}
	return slices.ContainsFunc(scorer.parts, func(other catalogTrack) bool {
		return other.title.hasPart() && other.title.part != track.title.part &&
			sameCompact(track.title.base, other.title.base)
	})
}

func preferUnarranged(matches []windowMatch) {
	if len(matches) < 2 || !matches[0].arrangedOriginal() {
		return
	}
	first := matches[0]
	for i, match := range matches[1:] {
		if first.prefersUnarranged(match) {
			matches[0], matches[i+1] = match, first
			return
		}
	}
}

func (m windowMatch) arrangedOriginal() bool {
	return m.track.arranged && m.score.quality == exactMatch && !m.score.title.version
}

func (m windowMatch) prefersUnarranged(other windowMatch) bool {
	if other.track.arranged || other.score.title.version {
		return false
	}
	return other.score.quality == exactMatch && sameCompact(m.track.title.base, other.track.title.base)
}

func competingPartial(matches []windowMatch, window []catalogTrack) bool {
	if len(matches) == 0 || matches[0].score.quality != partialMatch {
		return false
	}
	return matches[0].competingPartialCredit(window)
}

func (m windowMatch) competingPartialCredit(window []catalogTrack) bool {
	if m.score.artist.index < 0 {
		return true
	}
	return slices.ContainsFunc(window, m.matchesOtherPartialCredit)
}

func (m windowMatch) matchesOtherPartialCredit(track catalogTrack) bool {
	if !m.matchesTitleAtQuality(track) {
		return false
	}
	return slices.ContainsFunc(track.credits, func(credit matchPhrase) bool {
		return exactCredit(m.score.artist.partial, credit) != artistNone && !sameCompact(m.credit(), credit)
	})
}

func (m windowMatch) sameTitleAndCorrections(track catalogTrack) bool {
	if !m.sameTitle(track) {
		return false
	}
	return !slices.ContainsFunc(m.score.artist.corrections, func(edit artistEdit) bool {
		return !hasExactCredit(edit.credit, track.credits)
	})
}

func (m windowMatch) matchesOtherFuzzyCredit(track catalogTrack) bool {
	if len(m.score.artist.corrections) == 0 || !m.matchesTitleAtQuality(track) {
		return false
	}
	return slices.ContainsFunc(m.score.artist.corrections, func(edit artistEdit) bool {
		return slices.ContainsFunc(track.credits, edit.matchesOtherCredit)
	})
}

func (m windowMatch) matchesTitleAtQuality(track catalogTrack) bool {
	score := matchScore{title: track.title.match(m.score.fields.song), artist: m.score.artist, fields: m.score.fields}
	return score.title.matched && track.quality(score) == m.score.quality
}

func (e artistEdit) matchesOtherCredit(credit matchPhrase) bool {
	return !sameCompact(e.credit, credit) && matchArtist(e.query, credit).level != artistNone
}

func (s matchScore) eligible(window []catalogTrack) bool {
	if s.artist.level == artistNone || s.artist.exact() {
		return true
	}
	if knownCredit(s.fields.artist, window) || knownCredit(s.artist.partial, window) {
		return false
	}
	return !slices.ContainsFunc(s.artist.corrections, func(edit artistEdit) bool {
		return knownCredit(edit.query, window)
	})
}

func knownCredit(artist matchPhrase, window []catalogTrack) bool {
	if len(artist) == 0 {
		return false
	}
	return slices.ContainsFunc(window, func(track catalogTrack) bool {
		return !track.derivative && hasExactCredit(artist, track.credits)
	})
}

func compareWindowMatches(a, b windowMatch) int {
	if order := b.score.compare(a.score); order != 0 {
		return order
	}
	return cmp.Compare(a.score.versionPriority(), b.score.versionPriority())
}

func (s matchScore) versionPriority() int {
	if s.title.version && s.artist.level == artistNone {
		return 1
	}
	return 0
}

func (a artistMatch) exact() bool { return a.level == artistFull && a.edits == 0 }

func preferAlbum(matches []windowMatch) []windowMatch {
	first := make(map[string]int)
	out := matches[:0]
	for _, candidate := range matches {
		id := candidate.track.isrc
		if i, ok := first[id]; ok {
			out[i] = preferredRelease(out[i], candidate)
			continue
		}
		if id != "" {
			first[id] = len(out)
		}
		out = append(out, candidate)
	}
	return out
}

func preferredRelease(first, other windowMatch) windowMatch {
	if other.track.compilation < first.track.compilation && compareWindowMatches(other, first) == 0 {
		return other
	}
	return first
}

func ambiguousTop(matches []windowMatch) bool {
	if len(matches) < 2 || matches[0].score.quality > fuzzyMatch {
		return false
	}
	top := matches[0]
	return slices.ContainsFunc(matches[1:], top.conflicts)
}

func (m windowMatch) conflicts(other windowMatch) bool {
	if m.score.quality != other.score.quality || m.sameSong(other) {
		return false
	}
	if m.score.quality == partialMatch {
		return !sameCompact(m.credit(), other.credit())
	}
	if m.score.quality != fuzzyMatch {
		return false
	}
	if sameCompact(m.track.title.base, other.track.title.base) {
		return m.score.compare(other.score) == 0
	}
	return other.score.edits()-m.score.edits() <= 1
}
