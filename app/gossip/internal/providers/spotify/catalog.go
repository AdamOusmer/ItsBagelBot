// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"slices"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

type catalogTrack struct {
	track       gossiprpc.SpotifyTrack
	title       titleForms
	credits     []matchPhrase
	artistNames []string
	derivative  bool
	arranged    bool
	isrc        string
	compilation int
}

func catalogWindow(tracks []gossiprpc.SpotifyTrack) []catalogTrack {
	window := make([]catalogTrack, 0, len(tracks))
	for _, track := range tracks {
		window = append(window, readCatalogTrack(track))
	}
	return window
}

func readCatalogTrack(track gossiprpc.SpotifyTrack) catalogTrack {
	title := trackTitle(track.Name)
	names := slices.Clone(track.Artists)
	credits := creditWords(names)
	for _, feature := range title.features {
		if !hasExactCredit(matchWords(feature), credits) {
			names = append(names, feature)
		}
	}
	c := catalogTrack{track: track, title: title, artistNames: names, credits: creditWords(names)}
	c.readAlbum(matchWords(track.Album))
	for _, credit := range c.credits {
		c.readCredit(credit)
	}
	return c
}

func (c *catalogTrack) readAlbum(words matchPhrase) {
	for _, tag := range recordingTags(words) {
		roles := tag.form.term.roles
		c.derivative = c.derivative || roles&albumDerivative != 0
		if roles&albumLive != 0 && c.title.roles&sourceRelease != 0 {
			c.title.addTag(tag, words, 1)
			c.title.tail = slices.Concat(c.title.tail, tag.form.words)
		}
	}
}

func (c *catalogTrack) readCredit(words matchPhrase) {
	for _, tag := range recordingTags(words) {
		roles := tag.form.term.roles
		c.derivative = c.derivative || roles&derivativeCredit != 0
		c.arranged = c.arranged || roles&arrangerCredit != 0
		if roles&creditVersion != 0 && len(tag.form.words) == len(words) {
			c.title.addTag(tag, words, 1)
			c.title.tail = slices.Concat(c.title.tail, words)
		}
	}
}

func (c catalogTrack) score(fields searchFields, credits *creditMemo) matchScore {
	title := c.title.match(fields.song)
	if !title.matched {
		return c.scoreUnasked(fields, credits)
	}
	artist := credits.match(fields.artist, c.credits)
	if len(fields.artist) > 0 && artist.level == artistNone {
		return matchScore{}
	}
	score := matchScore{title: title, artist: artist, fields: fields}
	score.quality = c.quality(score)
	return score
}

func (c catalogTrack) quality(score matchScore) matchQuality {
	switch {
	case c.derivative && !c.hasRecordingEvidence(score):
		return blockedRecording
	case !c.hasRecordingEvidence(score):
		return noMatch
	case c.title.fallbackEdition() && !score.title.edition:
		return c.editionQuality(score)
	case score.artist.level == artistPartial:
		return partialMatch
	case score.title.edits+score.artist.edits > 0:
		return fuzzyMatch
	default:
		return exactMatch
	}
}

func (c catalogTrack) editionQuality(score matchScore) matchQuality {
	if c.title.edition && score.artist.level == artistNone {
		return c.title.unnamedEditionQuality()
	}
	return c.fallbackQuality(score.artist)
}

func (c catalogTrack) hasRecordingEvidence(score matchScore) bool {
	if c.derivative {
		return score.title.derivative && (score.artist.exact() || score.artist.level == artistNone)
	}
	if score.title.alias {
		return score.artist.exact()
	}
	return score.title.edits == 0 || score.title.version || score.artist.level != artistNone
}

func (c catalogTrack) scoreUnasked(fields searchFields, credits *creditMemo) matchScore {
	if !c.title.blocked {
		return matchScore{}
	}
	title := c.title.unaskedBase(fields.song)
	if !title.matched {
		return matchScore{}
	}
	artist := credits.match(fields.artist, c.credits)
	if len(fields.artist) > 0 && artist.level == artistNone {
		return matchScore{}
	}
	return matchScore{quality: c.unaskedQuality(fields.artist, artist), title: title, artist: artist, fields: fields}
}

func (c catalogTrack) unaskedQuality(query matchPhrase, artist artistMatch) matchQuality {
	if c.derivative {
		return blockedRecording
	}
	if c.title.hasPart() && len(c.title.recording) == 1 {
		return c.fallbackQuality(artist)
	}
	if c.requestedRemixCredit(query, artist) {
		return c.fallbackQuality(artist)
	}
	return blockedRecording
}

func (t titleForms) unaskedBase(song matchPhrase) titleResult {
	if sameCompact(song.withoutArticle(), t.base) || sameCompact(song, t.short) {
		return titleResult{matched: true}
	}
	return titleResult{}
}

func (c catalogTrack) requestedRemixCredit(query matchPhrase, artist artistMatch) bool {
	if len(c.title.recording) != 1 || c.title.roles&guestRemix == 0 {
		return false
	}
	return artist.exact() && c.hasRemixGuest(query)
}

func (c catalogTrack) fallbackQuality(artist artistMatch) matchQuality {
	if artist.exact() || artist.level == artistNone {
		return fallbackMatch
	}
	return blockedRecording
}

func (c catalogTrack) hasRemixGuest(artist matchPhrase) bool {
	if len(c.credits) < 2 || exactCredit(artist, c.credits[0]) == artistFull {
		return false
	}
	for _, group := range artistGroups(artist) {
		if hasExactCredit(group, c.credits[1:]) {
			return true
		}
	}
	return false
}

func (c catalogTrack) primaryCredit() matchPhrase {
	if len(c.credits) == 0 {
		return nil
	}
	return c.credits[0]
}
