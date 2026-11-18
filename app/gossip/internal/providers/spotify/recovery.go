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
	source := matchText(raw)
	variants := source.sourceWords().variants()
	candidates := originalTitleRecovery(source, variants[0], window)
	for _, variant := range variants {
		candidates = append(candidates, recoveryVariant(source, variant, window)...)
	}
	slices.SortStableFunc(candidates, func(a, b scoredCandidate) int { return cmp.Compare(b.score, a.score) })
	return uniqueRecoveryCandidates(candidates)
}

func recoveryVariant(raw matchText, words sourceWords, window []catalogTrack) []scoredCandidate {
	var candidates []scoredCandidate
	for _, split := range wordSplits(words.words) {
		candidates = append(candidates, split.recoveryCandidates(raw, words, window)...)
	}
	return candidates
}

func originalTitleRecovery(raw matchText, words sourceWords, window []catalogTrack) []scoredCandidate {
	if len(words.words) == 0 {
		return nil
	}
	if !slices.ContainsFunc(window, func(track catalogTrack) bool {
		return track.title.base.compact() == words.words.compact() && track.title.blocked
	}) {
		return nil
	}
	song := raw.queryText(words.source(wordRange{0, len(words.words)}))
	candidate := searchCandidate{q: `track:"` + song + `"`, name: viaFiltered, song: song, match: &searchFields{song: words.words}}
	return []scoredCandidate{{candidate, 0.5}}
}

func (s fieldSplit) recoveryCandidates(raw matchText, words sourceWords, window []catalogTrack) []scoredCandidate {
	song, artist := words.source(s.song), words.source(s.artist)
	if song.start < artist.end && artist.start < song.end {
		return nil
	}
	query := textSplit{left: raw.queryText(song), right: raw.queryText(artist)}
	fields := s.fields(words.words)
	var candidates []scoredCandidate
	candidates = appendRecovery(candidates, query, fields, window)
	if shifted, ok := fields.withTrailingVersion(); ok {
		cut := s.artist.start + len(shifted.artist)
		query.left += " " + raw.queryText(words.source(wordRange{cut, s.artist.end}))
		query.right = raw.queryText(words.source(wordRange{s.artist.start, cut}))
		candidates = appendRecovery(candidates, query, shifted, window)
	}
	return candidates
}

func appendRecovery(out []scoredCandidate, query textSplit, fields searchFields, window []catalogTrack) []scoredCandidate {
	candidate, ok := recoveryCandidate(query, fields)
	if !ok {
		return out
	}
	if scored, found := recoverySplit(candidate, window); found {
		return append(out, scored)
	}
	return out
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
		score = max(score, track.recoveryEvidence(fields))
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

func (c catalogTrack) recoveryEvidence(fields searchFields) float64 {
	if c.verifiedVersionRecovery(fields) {
		return 2
	}
	if c.title.originalRecoveryMatch(fields.song, artistNone) {
		return 1
	}
	return c.title.recoveryEvidence(fields.song)
}

func (c catalogTrack) verifiedVersionRecovery(fields searchFields) bool {
	level, _ := creditEvidence(fields.artist, c.credits)
	if level != artistFull {
		return false
	}
	request, _ := c.title.versionRequest(fields.song)
	return request.matched || c.title.originalRecoveryMatch(fields.song, level)
}

func (t titleForms) originalRecoveryMatch(song matchPhrase, artist artistLevel) bool {
	if !t.blocked || len(t.base) == 0 {
		return false
	}
	if artist == artistFull {
		_, ok := recoveryBaseIdentity(song, t.base)
		return ok
	}
	_, ok := titleIdentity(song, t.base)
	return ok
}

func recoveryBaseIdentity(song, base matchPhrase) (int, bool) {
	if conflictingRecording(song, base) {
		return 0, false
	}
	return phraseIdentity(song, base)
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
	if result.matched || t.blocked {
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
	c.findBaseSpelling(fields, song, queryField(title))
	for i, credit := range c.credits {
		artist.consider(fields.artist, spellingForm{credit, c.track.Artists[i]}, phraseIdentity)
	}
}

func (c catalogTrack) findBaseSpelling(fields searchFields, song *spellingMatch, title queryField) {
	level, _ := creditEvidence(fields.artist, c.credits)
	if !c.title.originalRecoveryMatch(fields.song, level) {
		return
	}
	base := titleDelimiter.Split(string(title), 2)[0]
	song.consider(fields.song, spellingForm{c.title.base, base}, recoveryBaseIdentity)
}

func (t titleForms) spellingForms() []matchPhrase {
	if t.blocked {
		return []matchPhrase{t.full, t.clean}
	}
	return []matchPhrase{t.full, t.clean, t.base}
}
