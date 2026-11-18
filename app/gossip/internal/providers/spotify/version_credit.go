// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import "slices"

var recordingCreditWords = map[string]bool{
	"nightcore": true, "daycore": true, "sped": true, "slowed": true, "karaoke": true,
	"8d": true, "lofi": true, "reverb": true, "tribute": true, "cover": true, "covers": true,
}

func creditVersions(credits []matchPhrase) matchPhrase {
	var version matchPhrase
	for i, credit := range credits {
		version = append(version, credit.recordingCredit(i == 0)...)
	}
	return version
}

func (p matchPhrase) recordingCredit(first bool) matchPhrase {
	if len(p) == 1 && !first {
		return p.recordingVersion()
	}
	var version matchPhrase
	for i, word := range p {
		if p.creditVersionToken(i) {
			version = append(version, word)
		}
	}
	return version
}

func (p matchPhrase) creditVersionToken(i int) bool {
	word := p[i]
	switch {
	case recordingCreditWords[word]:
		return true
	case p.versionPhraseToken(i):
		return true
	case recordingMarkers[word]:
		return genericVersionWord(p.wordAt(i + 1))
	default:
		return false
	}
}

func (c catalogTrack) recordingCreditConflict(fields searchFields) bool {
	if len(c.version) == 0 {
		return false
	}
	_, requested := c.title.versionRequest(fields.song)
	if hasExactCredit(fields.artist, c.credits) {
		requested = slices.Concat(requested, fields.artist)
	}
	return !requested.containsWords(c.version.requiredVersion())
}
