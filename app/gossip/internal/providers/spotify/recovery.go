// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"iter"
	"slices"
	"strings"
)

type scoredCandidate struct {
	candidate searchCandidate
	score     matchScore
}

func (s *textSearch) evidenceCandidates() []searchCandidate {
	var candidates []scoredCandidate
	for _, variant := range s.query.sources {
		candidates = append(candidates, s.recoveryVariant(variant)...)
	}
	slices.SortStableFunc(candidates, func(a, b scoredCandidate) int { return b.score.compare(a.score) })
	return uniqueRecoveryCandidates(candidates)
}

func (s *textSearch) recoveryVariant(words sourceWords) []scoredCandidate {
	var candidates []scoredCandidate
	positions := versionPositions(words.words)
	for _, split := range wordSplits(words.words) {
		candidates = s.addRecovery(candidates, split.recovery(s, words))
		candidates = s.addRecovery(candidates, split.shiftedRecovery(s, words, positions))
	}
	return candidates
}

func (s *textSearch) addRecovery(plan []scoredCandidate, recovery scoredCandidate) []scoredCandidate {
	if recovery.candidate.q == "" {
		return plan
	}
	if corrected, changed := correctFields(recovery.candidate, s.observed); changed {
		recovery.candidate = corrected
	}
	return append(plan, recovery)
}

func (split fieldSplit) recovery(search *textSearch, words sourceWords) scoredCandidate {
	fields := split.fields(words.words)
	score := search.recoveryScore(fields)
	if score.quality == noMatch {
		return scoredCandidate{}
	}
	return scoredCandidate{split.rawCandidate(search.query.raw, words), score}
}

func (split fieldSplit) shiftedRecovery(search *textSearch, words sourceWords, positions []int) scoredCandidate {
	fields, ok := split.shiftVersion(words.words, positions)
	if !ok {
		return scoredCandidate{}
	}
	score := search.recoveryScore(fields)
	if score.quality == noMatch {
		return scoredCandidate{}
	}
	cut := split.artist.start + len(fields.artist)
	raw := search.query.raw
	base := raw.queryText(words.source(split.song))
	suffix := raw.queryText(words.source(wordRange{cut, split.artist.end}))
	artist := raw.queryText(words.source(wordRange{split.artist.start, cut}))
	candidate, _ := recoveryCandidate(textSplit{left: base + " " + suffix, right: artist}, fields)
	return scoredCandidate{candidate, score}
}

func recoveryCandidate(text textSplit, fields searchFields) (searchCandidate, bool) {
	if text.right == "" && len(fields.song) > 0 {
		return trackCandidate(queryField(text.left), fields), true
	}
	result, ok := text.candidate()
	result.match = &fields
	return result, ok
}

func trackCandidate(title queryField, fields searchFields) searchCandidate {
	song := title.unquoted()
	return searchCandidate{q: `track:"` + song + `"`, name: viaFiltered, song: song, match: &fields}
}

func (s *textSearch) recoveryScore(fields searchFields) matchScore {
	var best matchScore
	for _, track := range s.observed {
		score := s.scorer.recoveryScore(track, fields)
		if score.compare(best) > 0 {
			best = score
		}
	}
	return best
}

func (scorer windowScorer) recoveryScore(track catalogTrack, fields searchFields) matchScore {
	score := scorer.score(track, fields)
	if score.quality != noMatch {
		return score
	}
	switch {
	case track.supportsVersionRetry(fields):
		return matchScore{quality: versionIntent}
	case track.title.match(fields.song).matched:
		return matchScore{quality: titleOnly}
	case properTitlePrefix(fields.song, track.title.full):
		return matchScore{quality: titlePrefix}
	case hasExactCredit(fields.artist, track.credits):
		return matchScore{quality: creditOnly}
	default:
		return matchScore{}
	}
}

func (c catalogTrack) supportsVersionRetry(fields searchFields) bool {
	suffix := c.title.versionSuffix(fields.song)
	return len(suffix) > 0 && c.matchesVersionIntent(fields.artist, suffix)
}

func (c catalogTrack) matchesVersionIntent(artist, suffix matchPhrase) bool {
	return hasExactCredit(artist, c.credits) && len(versionPositions(suffix)) > 0
}

func properTitlePrefix(prefix, title matchPhrase) bool {
	return len(prefix) >= 2 && len(prefix) < len(title) && slices.Equal(prefix, title[:len(prefix)])
}

func uniqueRecoveryCandidates(candidates []scoredCandidate) []searchCandidate {
	out := make([]searchCandidate, 0, len(candidates))
	seen := make(map[string]bool)
	for _, scored := range candidates {
		if seen[scored.candidate.q] {
			continue
		}
		seen[scored.candidate.q] = true
		out = append(out, scored.candidate)
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
	originalSong := spellingForm{fields.song, c.song}
	originalArtist := spellingForm{fields.artist, c.artist}
	title, credit := song.correction(originalSong), artist.correction(originalArtist)
	if title.same(originalSong) && credit.same(originalArtist) {
		return searchCandidate{}, false
	}
	result, ok := recoveryCandidate(textSplit{left: title.text, right: credit.text}, searchFields{title.words, fields.artist})
	if !sameCompact(credit.words, fields.artist) {
		result.confirmCredit = credit.words
	}
	result.corrected = ok
	return result, ok
}

func (c catalogTrack) findSpelling(fields searchFields, song, artist *spellingMatch) {
	if identity := c.title.recoveryIdentity(); identity != nil {
		song.consider(fields.song, spellingForm{c.title.full, c.track.Name}, identity)
	}
	song.consider(fields.song, spellingForm{c.title.base, c.title.baseText}, titleIdentity)
	for i, credit := range c.credits {
		artist.consider(fields.artist, spellingForm{credit, c.artistNames[i]}, artistIdentity)
	}
}

func (t titleForms) recoveryIdentity() func(matchPhrase, matchPhrase) (int, bool) {
	switch {
	case t.literalVersion:
		return t.versionIdentity
	case t.fallbackEdition():
		return nil
	default:
		return titleIdentity
	}
}

func (t titleForms) versionIdentity(query, _ matchPhrase) (int, bool) {
	version := t.requestedVersion(query)
	return version.edits, version.matched
}

func (s *textSearch) recoveryPlan() []searchCandidate {
	if s.remaining == 0 {
		return nil
	}
	top := s.best.top()
	if top.score.quality != noMatch && !top.unqualifiedDerivative() {
		return s.recoverMatch(top)
	}
	if len(s.query.explicit) > 0 {
		return s.recoverExplicit()
	}
	evidence := s.evidenceCandidates()
	if clean := s.query.raw.withoutEmotes(); clean != s.query.raw {
		evidence = slices.Insert(evidence, 0, searchCandidate{q: normalizeText(string(clean)), name: viaText})
	}
	return s.withRawFallback(evidence, top.unqualifiedDerivative())
}

func (s *textSearch) recoverMatch(top windowMatch) []searchCandidate {
	original := top.originalCandidate(s.query)
	switch top.score.quality {
	case partialMatch:
		return s.recoverPartial(top, original)
	case fuzzyMatch:
		return s.fuzzyRecovery(original, top)
	default:
		return []searchCandidate{original}
	}
}

func (m windowMatch) unqualifiedDerivative() bool {
	if m.score.artist.level != artistNone || m.score.title.version {
		return false
	}
	return m.track.derivative && len(m.score.fields.song) > 1
}

func (s *textSearch) recoverPartial(top windowMatch, original searchCandidate) []searchCandidate {
	if s.best.ambiguous {
		return []searchCandidate{original}
	}
	return partialRecovery(top, original)
}

func partialRecovery(top windowMatch, original searchCandidate) []searchCandidate {
	if top.score.artist.index < 0 {
		return []searchCandidate{original}
	}
	credit := normalizeText(top.track.artistNames[top.score.artist.index])
	confirmed, _ := recoveryCandidate(textSplit{left: original.song, right: credit}, original.fields())
	confirmed.confirmCredit = top.credit()
	if top.score.artist.combination {
		return []searchCandidate{original, confirmed}
	}
	return []searchCandidate{confirmed, original}
}

func (s *textSearch) fuzzyRecovery(original searchCandidate, top windowMatch) []searchCandidate {
	if s.best.ambiguous {
		return []searchCandidate{original}
	}
	corrected, ok := correctFields(original, s.observed)
	if !ok {
		return []searchCandidate{original}
	}
	if top.score.title.edits > 0 {
		return []searchCandidate{original, corrected, corrected.plainFallback()}
	}
	return []searchCandidate{corrected, original, corrected.plainFallback()}
}

func (m windowMatch) originalCandidate(query textQuery) searchCandidate {
	for _, explicit := range query.explicit {
		if explicit.otherFilters {
			return explicit
		}
	}
	fields := m.score.fields
	song := query.searchable(fields.song)
	artist := query.searchable(fields.artist)
	if song == "" {
		song = normalizeText(m.track.title.baseText)
	}
	if artist == "" && len(fields.artist) > 0 {
		artist = normalizeText(m.credit().text())
	}
	if artist == "" {
		return trackCandidate(queryField(song), fields)
	}
	candidate, _ := recoveryCandidate(textSplit{left: song, right: artist}, fields)
	return candidate
}

func (q textQuery) searchable(phrase matchPhrase) string {
	for _, source := range q.sources {
		if at := findPhrase(source.words, phrase); at >= 0 {
			return q.raw.queryText(source.source(wordRange{at, at + len(phrase)}))
		}
	}
	return ""
}

func findPhrase(words, phrase matchPhrase) int {
	if len(phrase) == 0 {
		return -1
	}
	for i := 0; i+len(phrase) <= len(words); i++ {
		if slices.Equal(words[i:i+len(phrase)], phrase) {
			return i
		}
	}
	return -1
}

func (s *textSearch) recoverExplicit() []searchCandidate {
	var candidates []searchCandidate
	for _, candidate := range s.query.explicit {
		if corrected, ok := correctFields(candidate, s.observed); ok {
			candidates = append(candidates, corrected, corrected.plainFallback())
		}
	}
	return candidates
}

func (s *textSearch) withRawFallback(evidence []searchCandidate, unqualifiedDerivative bool) []searchCandidate {
	var plan []searchCandidate
	for _, candidate := range evidence {
		plan = appendRetry(plan, candidate)
	}
	if len(s.observed) == 0 || unqualifiedDerivative {
		return s.appendRawRetries(plan)
	}
	if len(plan) >= s.remaining {
		return plan
	}
	for candidate := range s.rawSplits() {
		if !alreadyPlanned(candidate, plan) {
			return append(plan, candidate)
		}
	}
	return plan
}

func appendRetry(plan []searchCandidate, candidate searchCandidate) []searchCandidate {
	plan = append(plan, candidate)
	if candidate.corrected {
		plan = append(plan, candidate.plainFallback())
	}
	return plan
}

func (s *textSearch) appendRawRetries(plan []searchCandidate) []searchCandidate {
	for candidate := range s.rawSplits() {
		if len(plan) >= s.remaining {
			break
		}
		if !alreadyPlanned(candidate, plan) {
			plan = append(plan, candidate)
		}
	}
	return plan
}

func (s *textSearch) rawSplits() iter.Seq[searchCandidate] {
	return func(yield func(searchCandidate) bool) {
		if !s.query.raw.allowsRawSplit() {
			return
		}
		source := s.query.sources[len(s.query.sources)-1]
		splits := wordSplits(source.words)
		slices.SortStableFunc(splits, func(a, b fieldSplit) int { return a.song.start - b.song.start })
		for _, fields := range splits {
			if !s.yieldRaw(fields.rawCandidate(s.query.raw, source), yield) {
				return
			}
		}
	}
}

func (s *textSearch) yieldRaw(candidate searchCandidate, yield func(searchCandidate) bool) bool {
	if candidate.q == "" || s.used(candidate) {
		return true
	}
	return yield(candidate)
}

func alreadyPlanned(candidate searchCandidate, plan []searchCandidate) bool {
	return slices.ContainsFunc(plan, func(other searchCandidate) bool {
		return candidate.q == other.q
	})
}

func (s fieldSplit) rawCandidate(raw matchText, source sourceWords) searchCandidate {
	song, artist := source.source(s.song), source.source(s.artist)
	if song.start < artist.end && artist.start < song.end {
		return searchCandidate{}
	}
	parts := textSplit{left: raw.queryText(song), right: raw.queryText(artist)}
	candidate, _ := recoveryCandidate(parts, s.fields(source.words))
	return candidate
}

func (raw matchText) allowsRawSplit() bool {
	text := string(raw)
	return !raw.webToken() && !strings.HasSuffix(text, "-")
}
