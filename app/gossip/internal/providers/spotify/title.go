// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var titleDelimiter = regexp.MustCompile(`\s+[-–—‒/]\s+|,\s+|[([（［]|\s+["“]`)
var leadingParenthetical = regexp.MustCompile(`^[(（][^()（）]+[)）]\s*`)
var parentheticalTitle = regexp.MustCompile(`^[(（]([^()（）]+)[)）]`)
var inlineFeature = regexp.MustCompile(`(?i)\.\s+` + featurePattern + `\s+`)

type titleForms struct {
	full, base, tail, alternate, short matchPhrase
	baseText                           string
	recording                          []recordingTag
	tags                               []recordingTag
	roles                              recordingRole
	part                               int
	features                           []string
	blocked, edition, unknownDash      bool
	literalVersion                     bool
}

func trackTitle(name string) titleForms {
	name = strings.TrimSpace(name)
	leading := leadingParenthetical.FindStringIndex(name)
	start := 0
	if leading != nil {
		start = leading[1]
	}
	index := firstMetadataDelimiter(name[start:])
	if feature := inlineFeature.FindStringIndex(name[start:]); precedes(feature, index) {
		index = []int{feature[0], feature[0] + 1}
	}
	forms := titleForms{full: matchWords(name), baseText: name}
	forms.base = forms.full
	if index != nil && index[0]+start > 0 {
		forms.readTail(titleTail{base: name[:start+index[0]], delimiter: name[start+index[0] : start+index[1]], text: name[start+index[1]:]})
	}
	if start > 0 {
		forms.short = matchWords(forms.baseText[start:])
	}
	return forms
}

func precedes(candidate, first []int) bool {
	return candidate != nil && (first == nil || candidate[0] < first[0])
}

func firstMetadataDelimiter(name string) []int {
	delimiters := titleDelimiter.FindAllStringIndex(name, -1)
	for i, index := range delimiters {
		end := len(name)
		if i+1 < len(delimiters) {
			end = delimiters[i+1][0]
		}
		if metadataDelimiter(name[index[0]:index[1]], matchWords(name[index[1]:end])) {
			return index
		}
	}
	return nil
}

func metadataDelimiter(delimiter string, words matchPhrase) bool {
	switch strings.TrimSpace(delimiter) {
	case "/":
		return len(recordingTags(words)) > 0
	case ",":
		if len(words) < 2 {
			return false
		}
		form := recordingTerms.at(words)
		return form.term.roles&countedPart != 0 && readRecordingCount(words, recordingTag{form: form}).value >= 0
	default:
		return true
	}
}

type titleTail struct{ base, delimiter, text string }

func (t *titleForms) readTail(tail titleTail) {
	t.baseText = strings.TrimSpace(tail.base)
	t.base = matchWords(t.baseText)
	t.tail = matchWords(tail.text)
	t.readTailParts(tail)
	t.unknownDash = t.unknownDash && !t.blocked
	t.literalVersion = t.blocked
	if !t.blocked {
		t.alternate = parentheticalAlias(tail)
	}
}

func (t *titleForms) readTailParts(tail titleTail) {
	start, delimiter := 0, tail.delimiter
	for _, index := range tailDelimiters(tail.text) {
		t.readPart(titlePart{delimiter, tail.text[start:index[0]]})
		start, delimiter = index[1], tail.text[index[0]:index[1]]
	}
	t.readPart(titlePart{delimiter, tail.text[start:]})
}

func tailDelimiters(text string) [][]int {
	return slices.DeleteFunc(titleDelimiter.FindAllStringIndex(text, -1), func(index []int) bool {
		return text[index[0]] == ','
	})
}

func parentheticalAlias(tail titleTail) matchPhrase {
	alias := parentheticalTitle.FindStringSubmatch(tail.delimiter + tail.text)
	if alias == nil {
		return nil
	}
	words := matchWords(alias[1])
	if hasRecordingRole(words, sameRecording|fallbackEdition|naturalRerecord) {
		return nil
	}
	return words
}

type titlePart struct{ delimiter, text string }

func (p titlePart) unknownDash(words matchPhrase, tags []recordingTag) bool {
	return len(tags) == 0 && p.dashTail(words)
}

func (p titlePart) dashTail(words matchPhrase) bool {
	return len(words) > 0 && strings.ContainsAny(p.delimiter, "-–—‒")
}

func (t *titleForms) readPart(part titlePart) {
	words := matchWords(part.text)
	tags := recordingTags(words)
	t.unknownDash = t.unknownDash || part.unknownDash(words, tags)
	for _, tag := range tags {
		t.addTag(tag, words, len(tags))
	}
	if len(tags) > 0 && tags[0].form.term.roles&featureCredit != 0 {
		t.features = append(t.features, featureNames(part.text)...)
	}
}

func (t titleForms) fallbackEdition() bool { return t.edition || t.unknownDash }

func (t titleForms) unnamedEditionQuality() matchQuality {
	for _, tag := range t.tags {
		if tag.form.term.roles&fallbackEdition != 0 && slices.Equal(t.tail, tag.form.words) {
			return fallbackMatch
		}
	}
	return blockedRecording
}

func (t *titleForms) addTag(tag recordingTag, words matchPhrase, tagCount int) {
	roles := tag.form.term.roles
	if roles&bareVersion != 0 && tagCount > 1 {
		return
	}
	tag.count = readRecordingCount(words, tag).value
	t.tags = append(t.tags, tag)
	t.roles |= roles
	t.edition = t.edition || roles&fallbackEdition != 0
	if roles&countedPart != 0 {
		t.part = tag.count
	}
	if roles&differentRecording != 0 {
		t.blocked = true
		t.recording = append(t.recording, tag)
	}
}

var featurePattern = recordingRolePattern(featureCredit)
var featureLabel = regexp.MustCompile(`(?i)^\s*` + featurePattern + `\s+`)
var featureSeparator = regexp.MustCompile(`(?i)\s+(?:and|&|x)\s+|,\s+`)

func featureNames(part string) []string {
	cut := featureLabel.FindStringIndex(part)
	if cut == nil {
		return nil
	}
	return featureSeparator.Split(strings.Trim(part[cut[1]:], " )]）］.\""), -1)
}

type titleResult struct {
	edits      int
	matched    bool
	alias      bool
	version    bool
	edition    bool
	derivative bool
}

func (t titleForms) match(query matchPhrase) titleResult {
	explicit := t.requestedVersion(query)
	if t.blocked || explicit.matched {
		return explicit
	}
	best := scoreTitleForm(query, t.full)
	best = betterTitle(best, scoreTitleForm(query, t.base))
	best = betterTitle(best, scoreTitleForm(query, t.short))
	if len(t.alternate) > 0 && sameCompact(query, t.alternate) {
		best = betterTitle(best, titleResult{matched: true, alias: true})
	}
	return best
}

func betterTitle(first, second titleResult) titleResult {
	if second.matched && second.strongerThan(first) {
		return second
	}
	return first
}

func (t titleResult) strongerThan(other titleResult) bool {
	return !other.matched || t.edits < other.edits
}

func scoreTitleForm(query, form matchPhrase) titleResult {
	edits, ok := titleIdentity(query, form)
	if !ok {
		return titleResult{}
	}
	return titleResult{edits: edits, matched: true}
}

func titleIdentity(query, form matchPhrase) (int, bool) {
	query = query.withoutArticle()
	if len(query) == 0 || len(form) == 0 {
		return 0, false
	}
	if len(query) != len(form) && !sameCompact(query, form) {
		return 0, false
	}
	edits, ok := phraseIdentity(query, form)
	if !ok || edits == 0 {
		return edits, ok
	}
	return edits, plausibleTitleEdit(creditPair{query, form})
}

func plausibleTitleEdit(pair creditPair) bool {
	if len(pair.query) != 1 || len(pair.credit) != 1 {
		return true
	}
	query, title := pair.query[0], pair.credit[0]
	if inflectedTitle(query, title) || inflectedTitle(title, query) {
		return false
	}
	runes := runePair{[]rune(query), []rune(title)}
	if len(runes.b) > 6 || len(runes.a) != len(runes.b) {
		return true
	}
	return runes.hasTransposition() || runes.keyboardNeighbor()
}

func inflectedTitle(base, longer string) bool {
	if longer == base+"s" {
		return true
	}
	return strings.HasSuffix(base, "e") && longer == base+"r"
}

func (t titleForms) requestedVersion(song matchPhrase) titleResult {
	if t.roles&requestedRecordingRoles == 0 {
		return titleResult{}
	}
	if t.literalVersion && sameCompact(song, t.full) {
		return titleResult{matched: true, version: true, edition: t.edition, derivative: t.roles&(derivativeCredit|derivativeRecording) != 0}
	}
	suffix := t.versionSuffix(song)
	if len(suffix) > len(t.tail)+1 || len(suffix) == 0 {
		return titleResult{}
	}
	request := readVersionRequest(suffix)
	if !t.matchesVersion(request) {
		return titleResult{}
	}
	return titleResult{
		matched: true, edits: request.edits, version: true,
		edition: request.roles&fallbackEdition != 0, derivative: request.roles&(derivativeCredit|derivativeRecording) != 0,
	}
}

func (t titleForms) versionSuffix(song matchPhrase) matchPhrase {
	song = song.withoutArticle()
	cut := max(compactPrefix(song, t.base), compactPrefix(song, t.short))
	if cut == 0 {
		return nil
	}
	return song[cut:]
}

func compactPrefix(words, prefix matchPhrase) int {
	if len(prefix) == 0 || len(words) < 2 {
		return 0
	}
	left := prefix.compact()
	for i, word := range words[:len(words)-1] {
		if !strings.HasPrefix(left, word) {
			return 0
		}
		left = left[len(word):]
		if left == "" {
			return i + 1
		}
	}
	return 0
}

type versionRequest struct {
	tags   []recordingTag
	extras []int
	words  matchPhrase
	roles  recordingRole
	edits  int
}

func readVersionRequest(words matchPhrase) versionRequest {
	request := versionRequest{words: words}
	for i := 0; i < len(words); {
		form := recordingTerms.at(words[i:])
		if form.words == nil {
			form = requestedForm(words[i:], requestedRecordingRoles)
			request.noteEdit(form)
		}
		if form.words == nil {
			request.extras = append(request.extras, i)
			i++
			continue
		}
		request.addForm(words, recordingTag{form: form, at: i})
		i += len(form.words)
	}
	return request
}

func (r *versionRequest) noteEdit(form recordingForm) {
	if form.words != nil {
		r.edits++
	}
}

func (r *versionRequest) addForm(words matchPhrase, tag recordingTag) {
	tag.count = readRecordingCount(words, tag).value
	r.tags = append(r.tags, tag)
	r.roles |= tag.form.term.roles
	for _, extra := range recordingTerms.otherCJK(words[tag.at], tag.form) {
		r.tags = append(r.tags, recordingTag{form: extra})
		r.roles |= extra.term.roles
	}
}

func (t titleForms) matchesVersion(request versionRequest) bool {
	if request.roles&requestedRecordingRoles == 0 || !t.matchesExtras(request) {
		return false
	}
	return !slices.ContainsFunc(request.tags, t.missingVersion) &&
		!slices.ContainsFunc(t.recording, request.missingVersion)
}

func (t titleForms) matchesExtras(request versionRequest) bool {
	for _, at := range request.extras {
		if request.hasCountAt(at) {
			continue
		}
		if !slices.Contains(t.tail, request.words[at]) {
			return false
		}
	}
	return true
}

func (r versionRequest) hasCountAt(index int) bool {
	if r.roles&(countedPart|countedDuration) == 0 {
		return false
	}
	return slices.ContainsFunc(r.tags, func(tag recordingTag) bool {
		count := readRecordingCount(r.words, tag)
		return index >= count.span.start && index < count.span.end
	})
}

func (t titleForms) missingVersion(tag recordingTag) bool {
	return tag.form.term.roles&bareVersion == 0 && !hasVersion(t.tags, tag)
}

func (r versionRequest) missingVersion(tag recordingTag) bool {
	if tag.form.term.roles&tempoQualifier != 0 && r.roles&tempoChange != 0 {
		return false
	}
	return !hasVersion(r.tags, tag)
}

func hasVersion(tags []recordingTag, wanted recordingTag) bool {
	return slices.ContainsFunc(tags, func(tag recordingTag) bool {
		return tag.form.term.id == wanted.form.term.id && tag.count == wanted.count && tag.count >= 0
	})
}

type recordingCount struct {
	value int
	span  wordRange
}

func readRecordingCount(words matchPhrase, tag recordingTag) recordingCount {
	switch {
	case tag.form.term.roles&countedPart != 0:
		return countAt(words, tag.at+len(tag.form.words))
	case tag.form.term.roles&countedDuration != 0:
		return durationCount(words[:tag.at])
	default:
		return recordingCount{}
	}
}

func durationCount(words matchPhrase) recordingCount {
	if len(words) == 0 {
		return recordingCount{value: -1}
	}
	if count := countAt(words, max(0, len(words)-2)); count.span.end == len(words) {
		return count
	}
	return countAt(words, len(words)-1)
}

func countAt(words matchPhrase, at int) recordingCount {
	if at == len(words) {
		return recordingCount{value: -1}
	}
	span := wordRange{at, at + 1}
	if n, err := strconv.Atoi(words[at]); err == nil {
		return recordingCount{n, span}
	}
	if i := slices.Index(smallNumbers[:], words[at]); i >= 0 {
		return recordingCount{i, span}
	}
	i := slices.Index(tens[2:], words[at])
	if i < 0 {
		return recordingCount{value: -1}
	}
	n := 10 * (i + 2)
	if digit := countDigit(words[span.end:]); digit >= 0 {
		n += digit
		span.end++
	}
	return recordingCount{n, span}
}

func countDigit(words matchPhrase) int {
	if len(words) == 0 {
		return -1
	}
	return slices.Index(smallNumbers[:10], words[0])
}

func (t titleForms) hasPart() bool {
	return t.roles&countedPart != 0 && t.part >= 0
}
