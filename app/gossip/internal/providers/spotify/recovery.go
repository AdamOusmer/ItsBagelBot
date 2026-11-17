// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"cmp"
	"slices"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

type scoredCandidate struct {
	candidate searchCandidate
	score     float64
}

func recoveryCandidates(raw string, tracks []gossiprpc.SpotifyTrack) []searchCandidate {
	if len(tracks) == 0 {
		return nil
	}
	window := catalogWindow(tracks)
	var candidates []scoredCandidate
	source := matchText(raw)
	for _, variant := range source.sourceWords().variants() {
		candidates = append(candidates, recoveryVariant(source, variant, window)...)
	}
	slices.SortStableFunc(candidates, func(a, b scoredCandidate) int { return cmp.Compare(b.score, a.score) })
	return uniqueRecoveryCandidates(candidates)
}

func recoveryVariant(raw matchText, words sourceWords, window []catalogTrack) []scoredCandidate {
	var candidates []scoredCandidate
	for _, split := range wordSplits(words.words) {
		candidate, ok := split.candidate(raw, words)
		if !ok {
			continue
		}
		if scored, found := recoverySplit(candidate, window); found {
			candidates = append(candidates, scored)
		}
	}
	return candidates
}

func (s fieldSplit) candidate(raw matchText, words sourceWords) (searchCandidate, bool) {
	song, artist := words.source(s.song), words.source(s.artist)
	if song.start < artist.end && artist.start < song.end {
		return searchCandidate{}, false
	}
	query := textSplit{left: raw.queryText(song), right: raw.queryText(artist)}
	return recoveryCandidate(query, s.fields(words.words))
}

func recoveryCandidate(text textSplit, fields searchFields) (searchCandidate, bool) {
	result, ok := text.candidate()
	result.match = &fields
	return result, ok
}

func recoverySplit(candidate searchCandidate, window []catalogTrack) (scoredCandidate, bool) {
	fields := candidate.fields()
	score := 0.0
	for _, track := range window {
		score = max(score, track.title.recoveryEvidence(fields.song))
	}
	if score == 0 {
		return scoredCandidate{}, false
	}
	corrected, changed := correctFields(candidate, window)
	if changed {
		candidate = corrected
	}
	return scoredCandidate{candidate, score}, true
}

func (t titleForms) recoveryEvidence(song matchPhrase) float64 {
	if t.recoveryMatch(song).matched {
		return 1
	}
	title := t.full
	if len(song) < 2 || !properTitlePrefix(song, title) {
		return 0
	}
	matched := 0
	for _, word := range title {
		if slices.Contains(song, word) {
			matched++
		}
	}
	return float64(len(song)+matched) / float64(len(song)+len(title))
}

func properTitlePrefix(prefix, title matchPhrase) bool {
	return len(prefix) < len(title) && slices.Equal(prefix, title[:len(prefix)])
}

func (t titleForms) recoveryMatch(song matchPhrase) titleResult {
	result := t.literalMatch(song)
	if result.matched || blocksBaseTitle(t.tail) {
		return result
	}
	return scoreTitleForm(song, t.base, titleResult{points: 94, base: true})
}

func uniqueRecoveryCandidates(candidates []scoredCandidate) []searchCandidate {
	out := make([]searchCandidate, 0, maxRecoverySearches)
	seen := make(map[string]bool)
	for _, scored := range candidates {
		if seen[scored.candidate.q] {
			continue
		}
		seen[scored.candidate.q] = true
		out = append(out, scored.candidate)
		if len(out) == maxRecoverySearches {
			break
		}
	}
	return out
}

type spellingForm struct {
	words matchPhrase
	text  string
}

func (f spellingForm) same(other spellingForm) bool {
	return f.text == other.text && slices.Equal(f.words, other.words)
}

type spellingMatch struct {
	spellingForm
	edits int
	tied  bool
}

func (m *spellingMatch) consider(query matchPhrase, form spellingForm, identity func(matchPhrase, matchPhrase) (int, bool)) {
	edits, ok := identity(query, form.words)
	if !ok {
		return
	}
	if m.words == nil || edits < m.edits {
		*m = spellingMatch{spellingForm: form, edits: edits}
		return
	}
	if edits == m.edits && !slices.Equal(m.words, form.words) {
		m.tied = true
	}
}

func (m spellingMatch) correction(query spellingForm) spellingForm {
	if m.words != nil && !m.tied {
		return spellingForm{m.words, normalizeText(queryField(m.text).unquoted())}
	}
	return query
}

func correctFields(candidate searchCandidate, window []catalogTrack) (searchCandidate, bool) {
	if !candidate.hasFields() || candidate.otherFilters {
		return searchCandidate{}, false
	}
	fields := candidate.fields()
	var song, artist spellingMatch
	for _, track := range window {
		track.findSpelling(fields, &song, &artist)
	}
	return candidate.withCorrection(song, artist)
}

func (c searchCandidate) withCorrection(song, artist spellingMatch) (searchCandidate, bool) {
	fields := c.fields()
	originalSong := spellingForm{fields.song, searchableText(c.song)}
	originalArtist := spellingForm{fields.artist, searchableText(c.artist)}
	title, credit := song.correction(originalSong), artist.correction(originalArtist)
	if title.same(originalSong) && credit.same(originalArtist) {
		return searchCandidate{}, false
	}
	result, ok := recoveryCandidate(textSplit{left: title.text, right: credit.text}, searchFields{title.words, credit.words})
	result.corrected = ok
	return result, ok
}

func (c catalogTrack) findSpelling(fields searchFields, song, artist *spellingMatch) {
	title := cleanTrackTitle(queryField(c.track.Name).unquoted())
	for _, form := range c.title.spellingForms() {
		song.consider(fields.song, spellingForm{form, title}, titleIdentity)
	}
	for i, credit := range c.credits {
		artist.consider(fields.artist, spellingForm{credit, c.track.Artists[i]}, phraseIdentity)
	}
}

func (t titleForms) spellingForms() []matchPhrase {
	if blocksBaseTitle(t.tail) {
		return []matchPhrase{t.full, t.clean}
	}
	return []matchPhrase{t.full, t.clean, t.base}
}
