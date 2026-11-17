// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

var recordingMarkers = map[string]bool{
	"live": true, "acoustic": true, "remix": true, "mix": true, "cover": true,
	"instrumental": true, "karaoke": true, "demo": true, "session": true, "sped": true,
	"slowed": true, "reverb": true, "nightcore": true, "reprise": true, "interlude": true,
	"intro": true, "outro": true, "skit": true, "medley": true, "mashup": true,
	"part": true, "pt": true, "ii": true, "iii": true, "unplugged": true,
	"extended": true, "acappella": true, "piano": true, "orchestral": true, "lofi": true,
	"vip": true, "bootleg": true, "rework": true, "edit": true, "tribute": true,
}

var releaseSuffix = regexp.MustCompile(`(?i)(?:\s+[-–—]\s*|\s*\()(?:\d{4}\s+)?(?:re-?master(?:ed)?(?:\s+\d{4})?|mono|stereo|original\s+mix|(?:radio|single|album)\s+(?:edit|version)|(?:explicit|clean)(?:\s+version)?)(?:\s*\))?$`)
var featuredSuffix = regexp.MustCompile(`(?i)(?:\s+[-–—]\s*|\s*\(|\.\s*)(?:feat\.?|ft\.?|featuring)\s+([^()]+)\)?$`)
var titleDelimiter = regexp.MustCompile(` [-–—] |[([]`)
var parentheticalTitle = regexp.MustCompile(`^\(([^()]+)\)\s*$`)

func cleanTrackTitle(name string) string {
	name = releaseSuffix.ReplaceAllString(name, "")
	feature := featuredSuffix.FindStringSubmatchIndex(name)
	if feature != nil && !titleDelimiter.MatchString(name[feature[2]:feature[3]]) {
		return name[:feature[0]]
	}
	return name
}

type titleForms struct {
	full, clean, base, tail, alternate matchPhrase
}

func trackTitle(name string) titleForms {
	clean := cleanTrackTitle(name)
	forms := titleForms{full: matchWords(name), clean: matchWords(clean)}
	forms.base = forms.clean
	location := titleDelimiter.FindStringIndex(clean)
	if location == nil {
		return forms
	}
	forms.base = matchWords(clean[:location[0]])
	forms.tail = matchWords(clean[location[1]:])
	if alternate := parentheticalTitle.FindStringSubmatch(clean[location[0]:]); alternate != nil {
		forms.alternate = matchWords(alternate[1])
	}
	return forms
}

type titleResult struct {
	points  int
	edits   int
	base    bool
	matched bool
}

func (t titleForms) match(query matchPhrase, artist artistLevel) titleResult {
	literal := t.literalMatch(query)
	if literal.matched || !t.hasBase() {
		return literal
	}
	harmless := harmlessTail(t.tail)
	if artist == artistNone && !harmless {
		return titleResult{}
	}
	base := scoreTitleForm(query, t.base, titleResult{points: 94, base: true})
	if artist == artistFull && t.allowsAlternate(query) {
		return betterTitle(base, scoreTitleForm(query, t.alternate, titleResult{points: 94, base: true}))
	}
	return base
}

func (t titleForms) hasBase() bool {
	return len(t.tail) > 0 && !blocksBaseTitle(t.tail)
}

func (t titleForms) allowsAlternate(query matchPhrase) bool {
	return !harmlessTail(t.tail) && !conflictingRecording(query, t.clean)
}

func betterTitle(first, second titleResult) titleResult {
	if !second.matched {
		return first
	}
	if !first.matched || second.points > first.points {
		return second
	}
	return first
}

func (t titleForms) literalMatch(query matchPhrase) titleResult {
	full := scoreTitleForm(query, t.full, titleResult{points: 100})
	if slices.Equal(t.full, t.clean) {
		return full
	}
	clean := scoreTitleForm(query, t.clean, titleResult{points: 98})
	return betterTitle(full, clean)
}

func scoreTitleForm(query, form matchPhrase, result titleResult) titleResult {
	edits, ok := titleIdentity(query, form)
	if !ok {
		return titleResult{}
	}
	result.points -= 3 * edits
	result.edits = edits
	result.matched = true
	return result
}

func titleIdentity(query, form matchPhrase) (int, bool) {
	if conflictingRecording(query, form) {
		return 0, false
	}
	edits, ok := phraseIdentity(query, form)
	return edits, ok && (edits == 0 || utf8.RuneCountInString(form.compact()) >= 8)
}

var harmlessTitleTails = map[string]bool{
	"taylors version": true, "from": true, "feat": true, "ft": true, "with": true,
	"bonus track": true, "deluxe": true, "original": true, "soundtrack": true, "theme from": true,
}

func harmlessTail(tail matchPhrase) bool {
	for n := 1; n <= min(2, len(tail)); n++ {
		if harmlessTitleTails[tail[:n].text()] {
			return true
		}
	}
	return len(tail) > 0 && strings.HasPrefix(tail[0], "remaster")
}

func conflictingRecording(query, metadata matchPhrase) bool {
	for marker := range recordingMarkers {
		if slices.Contains(query, marker) != slices.Contains(metadata, marker) {
			return true
		}
	}
	return false
}

var versionWords = map[string]bool{
	"version": true, "audio": true, "stripped": true, "cappella": true, "acapella": true,
	"recorded": true, "rerecorded": true, "8d": true, "tiktok": true,
}

var versionPrefixes = [...]string{"remix", "slow", "mix", "speed", "sped", "live", "acoustic", "cover"}

func blocksBaseTitle(tail matchPhrase) bool {
	if len(tail) >= 2 && tail[:2].text() == "taylors version" {
		tail = tail[2:]
	}
	return slices.ContainsFunc(tail, versionWord)
}

func versionWord(word string) bool {
	return versionWords[word] || recordingMarkers[word] ||
		slices.ContainsFunc(versionPrefixes[:], func(prefix string) bool { return strings.HasPrefix(word, prefix) })
}
