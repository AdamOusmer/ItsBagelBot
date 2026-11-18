// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"cmp"
	"slices"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

type searchFields struct{ song, artist matchPhrase }

var artistStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "i": true, "it": true,
	"by": true, "of": true, "to": true, "in": true, "and": true,
}

func (v queryVariant) interpretations() []searchFields {
	var splits []searchFields
	for _, split := range wordSplits(v.words) {
		fields := split.fields(v.words)
		splits = append(splits, fields)
		if shifted, ok := fields.withTrailingVersion(); ok {
			splits = append(splits, shifted)
		}
	}
	if v.titleOnly {
		return append([]searchFields{{song: v.words}}, splits...)
	}
	return splits
}

type fieldSplit struct{ song, artist wordRange }

func (s fieldSplit) fields(words matchPhrase) searchFields {
	return searchFields{song: s.song.words(words), artist: s.artist.words(words)}
}

func wordSplits(words matchPhrase) []fieldSplit {
	var out []fieldSplit
	for i := len(words) - 1; i >= 1; i-- {
		left, right := wordRange{0, i}, wordRange{i, len(words)}
		out = append(out, fieldSplit{left, right}, fieldSplit{right, left})
	}
	return slices.DeleteFunc(out, func(split fieldSplit) bool {
		return artistStopwords[split.artist.words(words).withoutArticle().text()]
	})
}

type matchScore struct {
	points int
	artist artistLevel
	fuzzy  bool
	passed bool
	retry  *searchFields
}

type catalogTrack struct {
	track   gossiprpc.SpotifyTrack
	title   titleForms
	credits []matchPhrase
	version matchPhrase
	best    matchScore
}

func catalogWindow(tracks []gossiprpc.SpotifyTrack) []catalogTrack {
	window := make([]catalogTrack, 0, len(tracks))
	for _, track := range tracks {
		credits := creditWords(track.Artists)
		window = append(window, catalogTrack{track: track, title: trackTitle(track.Name), credits: credits, version: creditVersions(credits)})
	}
	return window
}

func (c catalogTrack) score(fields searchFields) matchScore {
	if c.recordingCreditConflict(fields) {
		return matchScore{}
	}
	level, edits := creditEvidence(fields.artist, c.credits)
	if len(fields.artist) == 0 {
		return combineScore(c.matchTitle(fields.song, artistNone), artistNone, 0)
	}
	if level == artistFull && len(fields.song) == 0 {
		return matchScore{points: 4 * (100 - 3*edits), artist: level, fuzzy: edits > 0, passed: true}
	}
	title := c.matchTitle(fields.song, artistFull)
	if !title.matched {
		return c.provisionalScore(fields, level, edits)
	}
	if !c.allowsArtist(level) {
		return matchScore{}
	}
	return combineScore(title, level, edits)
}

func (c catalogTrack) allowsArtist(level artistLevel) bool {
	return level == artistFull || (level == artistPartial && !c.title.blocked)
}

func (c catalogTrack) matchTitle(song matchPhrase, artist artistLevel) titleResult {
	title := c.title.match(song, artist)
	if title.matched || len(c.version) == 0 {
		return title
	}
	if c.title.blocked {
		return titleResult{}
	}
	version := c.title
	version.tail = c.version
	return version.matchRecording(song, artist)
}

func (c catalogTrack) provisionalScore(fields searchFields, level artistLevel, edits int) matchScore {
	if level != artistFull || !c.permitsFallback(fields.artist) {
		return matchScore{}
	}
	title := scoreTitleForm(fields.song, c.title.base, titleResult{points: 94, base: true})
	score := combineScore(title, level, edits)
	if score.passed {
		score.retry = &searchFields{song: c.title.base, artist: fields.artist}
	}
	return score
}

func (c catalogTrack) permitsFallback(artist matchPhrase) bool {
	if alternateEditions[c.title.tail.text()] {
		return true
	}
	return c.title.remixOnly() && c.hasRemixGuest(artist)
}

func (c catalogTrack) hasRemixGuest(artist matchPhrase) bool {
	if len(c.credits) < 2 || exactCredit(artist, c.credits[0]) == artistFull {
		return false
	}
	return hasExactCredit(artist, c.credits[1:])
}

func hasExactCredit(artist matchPhrase, credits []matchPhrase) bool {
	return slices.ContainsFunc(credits, func(credit matchPhrase) bool {
		return exactCredit(artist, credit) == artistFull
	})
}

func combineScore(title titleResult, level artistLevel, edits int) matchScore {
	if !title.qualifies(level) {
		return matchScore{}
	}
	switch level {
	case artistNone:
		return matchScore{points: 5 * title.points, fuzzy: title.base, passed: true}
	case artistPartial:
		return matchScore{points: 6*title.points + 4*85, artist: level, fuzzy: true, passed: true}
	default:
		return matchScore{points: 6*title.points + 4*(100-3*edits), artist: level, fuzzy: title.edits+edits > 0 || title.base, passed: true}
	}
}

func (t titleResult) qualifies(level artistLevel) bool {
	if !t.matched {
		return false
	}
	switch level {
	case artistNone:
		return t.edits == 0
	case artistPartial:
		return t.edits == 0 && !t.base
	default:
		return true
	}
}

func scoreInterpretation(window []catalogTrack, fields searchFields) {
	scores := make([]matchScore, len(window))
	fullByTitle := make(map[string]bool)
	for i, track := range window {
		score := track.score(fields)
		scores[i] = score
		base := track.title.base.compact()
		fullByTitle[base] = fullByTitle[base] || score.artist == artistFull
	}
	for i, score := range scores {
		track := &window[i]
		track.improve(score, fullByTitle[track.title.base.compact()])
	}
}

func (c *catalogTrack) improve(score matchScore, fullCredit bool) {
	if score.eligible(fullCredit) && score.compare(c.best) > 0 {
		c.best = score
	}
}

func (s matchScore) eligible(fullCredit bool) bool {
	return s.artist != artistPartial || !fullCredit
}

func (s matchScore) compare(other matchScore) int {
	if order := cmp.Compare(s.priority(), other.priority()); order != 0 {
		return order
	}
	return cmp.Compare(s.points, other.points)
}

func (s matchScore) priority() int {
	switch {
	case !s.passed:
		return 0
	case s.artist == artistNone:
		return 1
	case s.retry != nil:
		return 2
	default:
		return 3
	}
}

type rankedWindow struct {
	tracks    []gossiprpc.SpotifyTrack
	artist    artistLevel
	ambiguous bool
	retry     *searchFields
}

func (w rankedWindow) hasMatch() bool {
	return len(w.tracks) > 0 || w.ambiguous
}

type windowScores []catalogTrack

func rankPlain(tracks []gossiprpc.SpotifyTrack, query string) rankedWindow {
	ranked := rankVariants(tracks, matchText(query).queryVariants())
	for i, track := range ranked {
		tracks[i] = track.track
	}
	return ranked.passing()
}

func rankVariants(tracks []gossiprpc.SpotifyTrack, variants []queryVariant) windowScores {
	for _, variant := range variants {
		window := rankWindow(tracks, variant.interpretations())
		if window.topScore().passed {
			return window
		}
	}
	return nil
}

func rankWindow(tracks []gossiprpc.SpotifyTrack, fields []searchFields) windowScores {
	window := catalogWindow(tracks)
	for _, interpretation := range fields {
		scoreInterpretation(window, interpretation)
	}
	slices.SortStableFunc(window, func(a, b catalogTrack) int { return b.best.compare(a.best) })
	return window
}

func (window windowScores) topScore() matchScore {
	if len(window) == 0 {
		return matchScore{}
	}
	return window[0].best
}

func (window windowScores) passing() rankedWindow {
	if !window.topScore().passed {
		return rankedWindow{}
	}
	top := window[0]
	result := rankedWindow{artist: top.best.artist, retry: top.best.retry}
	for _, track := range window {
		if !track.best.passed || top.best.differentRecordingPriority(track.best) {
			break
		}
		if top.nearDifferentSong(track) {
			return rankedWindow{ambiguous: true}
		}
		result.tracks = append(result.tracks, track.track)
	}
	return result
}

func (c catalogTrack) nearDifferentSong(other catalogTrack) bool {
	gap := c.best.points - other.best.points
	return c.best.fuzzy && max(gap, -gap) <= 30 && c.title.base.compact() != other.title.base.compact()
}

func (s matchScore) differentRecordingPriority(other matchScore) bool {
	return (s.retry == nil) != (other.retry == nil)
}
