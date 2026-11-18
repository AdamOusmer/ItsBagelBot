// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"slices"
	"strings"
)

var versionWords = map[string]bool{
	"version": true, "audio": true, "stripped": true, "cappella": true, "capella": true,
	"acapella": true, "recorded": true, "rerecorded": true, "8d": true, "tiktok": true,
	"remixed": true, "slow": true, "speed": true, "covers": true, "daycore": true,
	"rmx": true, "rendition": true, "loop": true, "vocals": true, "boosted": true,
	"screwed": true, "vivo": true,
}

var alternateEditions = map[string]bool{
	"radio edit": true, "single version": true, "album version": true,
}

func (p matchPhrase) wordAt(index int) string {
	if index < 0 || index >= len(p) {
		return ""
	}
	return p[index]
}

func (p matchPhrase) versionToken(index int) bool {
	return recordingMarkers[p[index]] || versionWords[p[index]] || p.versionPhraseToken(index)
}

var versionPhrases = map[string]bool{
	"a cappella": true, "a capella": true, "taylors version": true, "re recorded": true,
}

func (p matchPhrase) versionPhraseToken(index int) bool {
	prev := p.wordAt(index - 1)
	switch p[index] {
	case "minute", "minutes":
		return numberTokens[prev]
	case "up", "down":
		return prev == "sped" || prev == "slowed"
	default:
		phrase := p[index] + " " + p.wordAt(index+1)
		return alternateEditions[phrase] || versionPhrases[phrase] || p.durationNumber(index)
	}
}

func (p matchPhrase) durationNumber(index int) bool {
	if !numberTokens[p[index]] {
		return false
	}
	if durationUnit(p.wordAt(index + 1)) {
		return true
	}
	if !slices.Contains(tens[2:], p[index]) {
		return false
	}
	return slices.Contains(smallNumbers[1:10], p.wordAt(index+1)) && durationUnit(p.wordAt(index+2))
}

func durationUnit(word string) bool {
	return word == "minute" || word == "minutes"
}

func (p matchPhrase) releaseLabel(index int) bool {
	switch p[index] {
	case "taylors":
		return p.wordAt(index+1) == "version"
	case "version":
		return p.wordAt(index-1) == "taylors"
	default:
		return false
	}
}

func (p matchPhrase) recordingVersion() matchPhrase {
	var version matchPhrase
	for i, word := range p {
		if p.versionToken(i) && !p.releaseLabel(i) {
			version = append(version, word)
		}
	}
	return version
}

func (p matchPhrase) containsWords(words matchPhrase) bool {
	return !slices.ContainsFunc(words, func(word string) bool { return !slices.Contains(p, word) })
}

func (p matchPhrase) versionQualifiers() matchPhrase {
	return slices.DeleteFunc(slices.Clone(p), genericVersionWord)
}

func genericVersionWord(word string) bool {
	return word == "version" || word == "audio"
}

func (p matchPhrase) requiredVersion() matchPhrase {
	words := p.recordingVersion().versionQualifiers()
	if slices.Contains(words, "slowed") || slices.Contains(words, "sped") {
		return slices.DeleteFunc(words, func(word string) bool { return word == "reverb" })
	}
	return words
}

func (t titleForms) requestedVersion(song matchPhrase) titleResult {
	title, requested := t.versionRequest(song)
	if !title.matched || !t.tail.containsWords(requested.versionQualifiers()) {
		return titleResult{}
	}
	if !requested.containsWords(t.tail.requiredVersion()) || t.hasUnaskedTail(requested) {
		return titleResult{}
	}
	return title
}

func (t titleForms) versionRequest(song matchPhrase) (titleResult, matchPhrase) {
	cut := song.versionCut(t.base)
	if cut == 0 || cut >= len(song) {
		return titleResult{}, nil
	}
	base := scoreTitleForm(song[:cut], t.base, titleResult{points: 94, base: true})
	if !base.matched {
		return titleResult{}, nil
	}
	suffix := song[cut:].correctVersionWords()
	if len(suffix.requiredVersion()) == 0 {
		return titleResult{}, nil
	}
	return base, suffix
}

func (p matchPhrase) versionCut(base matchPhrase) int {
	remaining := base.compact()
	for i, word := range p {
		if !strings.HasPrefix(remaining, word) {
			break
		}
		remaining = strings.TrimPrefix(remaining, word)
		if remaining == "" {
			return i + 1
		}
	}
	return len(base)
}

func (p matchPhrase) correctVersionWords() matchPhrase {
	words := slices.Clone(p)
	for i := range words {
		words[i] = words.correctedVersionWord(i)
	}
	return words
}

func (p matchPhrase) correctedVersionWord(index int) string {
	word := p[index]
	if recordingMarkers[word] || versionWords[word] {
		return word
	}
	if p.wordAt(index-1) == "sped" && resemblesSpedUp(word) {
		return "up"
	}
	return closestVersionMarker(word)
}

func resemblesSpedUp(word string) bool {
	_, ok := wordIdentity(wordPair{query: "sped" + word, metadata: "spedup"})
	return ok
}

func closestVersionMarker(word string) string {
	if word == "life" || protectedWord([]rune(word)) {
		return word
	}
	best, distance := word, 3
	for marker := range recordingMarkers {
		edits, ok := wordIdentity(wordPair{query: word, metadata: marker, version: true})
		switch {
		case !ok || edits > distance:
		case edits == distance:
			best = word
		default:
			best, distance = marker, edits
		}
	}
	return best
}

func (t titleForms) remixOnly() bool {
	if t.hasUnaskedTail(nil) {
		return false
	}
	version := t.tail.recordingVersion()
	return len(version) > 0 && !slices.ContainsFunc(version, func(word string) bool {
		return !strings.HasPrefix(word, "remix")
	})
}

func (f searchFields) withTrailingVersion() (searchFields, bool) {
	cut := len(f.artist)
	for cut > 0 && f.artist.versionToken(cut-1) {
		cut--
	}
	if cut == 0 || cut == len(f.artist) {
		return searchFields{}, false
	}
	return searchFields{song: slices.Concat(f.song, f.artist[cut:]), artist: f.artist[:cut]}, true
}
