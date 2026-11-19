// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type recordingRole uint32

const (
	differentRecording recordingRole = 1 << iota
	sameRecording
	fallbackEdition
	derivativeCredit
	naturalRerecord
	creditVersion
	albumDerivative
	featureCredit
	arrangerCredit
	exactVersion
	bareVersion
	tempoChange
	tempoQualifier
	countedPart
	countedDuration
	guestRemix
	albumLive
	sourceRelease
	derivativeRecording
)

type recordingTerm struct {
	id    string
	roles recordingRole
	forms string
}

var recordingVocabulary = []recordingTerm{
	{"live", differentRecording | albumLive, "live|en vivo|ao vivo|en directo|concierto|concert|konzert|концерт|ライブ|라이브"},
	{"remix", differentRecording | guestRemix, "remix|remixed|rmx|remezcla|ремикс|リミックス|리믹스"},
	{"mix", differentRecording, "mix"},
	{"rework", differentRecording, "rework"},
	{"refix", differentRecording, "refix"},
	{"vip", differentRecording, "vip"},
	{"flip", differentRecording, "flip"},
	{"bootleg", differentRecording, "bootleg"},
	{"dub", differentRecording, "dub"},
	{"edit", differentRecording, "edit"},
	{"sped up", differentRecording | creditVersion | tempoChange, "sped up|speed up"},
	{"slowed", differentRecording | creditVersion | tempoChange, "slowed down|slowed"},
	{"nightcore", differentRecording | creditVersion, "nightcore"},
	{"daycore", differentRecording | creditVersion, "daycore"},
	{"reverb", differentRecording | creditVersion | tempoQualifier, "reverb"},
	{"chopped and screwed", differentRecording, "chopped and screwed"},
	{"8d", differentRecording | creditVersion, "8d"},
	{"bass boosted", differentRecording, "bass boosted"},
	{"phonk", differentRecording, "phonk"},
	{"lofi", differentRecording | arrangerCredit, "lofi|lo fi"},
	{"acoustic", differentRecording, "acoustic|acustico|acoustique|akustik|акустика|アコースティック|어쿠스틱|unplugged"},
	{"stripped", differentRecording, "stripped"},
	{"piano", differentRecording | arrangerCredit, "piano"},
	{"piano", differentRecording | derivativeCredit | albumDerivative, "piano versions|piano version"},
	{"orchestral", differentRecording | arrangerCredit, "orchestral|symphonic|orchestra|strings|quartet|ensemble"},
	{"instrumental", differentRecording, "instrumental|инструментал|インスト|playback|backing track|minus one"},
	{"karaoke", differentRecording | derivativeCredit | albumDerivative, "karaoke|караоке|カラオケ|노래방"},
	{"a cappella", differentRecording, "a cappella|a capella|acappella|acapella|vocals only|off vocal"},
	{"cover", differentRecording | exactVersion | arrangerCredit | derivativeRecording, "cover|covers|кавер|커버|カバー"},
	{"tribute", differentRecording | arrangerCredit | albumDerivative | derivativeRecording, "tribute|homenaje|homenagem|hommage"},
	{"tribute", differentRecording | derivativeCredit | albumDerivative, "tribute to|tribute band"},
	{"cover", differentRecording | derivativeCredit | albumDerivative, "cover version|cover versions|covers collection|cover band|cover artist"},
	{"lullaby", differentRecording | arrangerCredit | albumDerivative | derivativeRecording, "lullaby|lullabies"},
	{"rendition", differentRecording | arrangerCredit | albumDerivative | derivativeRecording, "rendition|renditions"},
	{"parody", differentRecording | arrangerCredit | albumDerivative | derivativeRecording, "parody"},
	{"derivative", differentRecording | derivativeCredit | albumDerivative, "in the style of|originally performed by|made famous by|music box|8 bit|soundalike"},
	{"tiktok", differentRecording, "tiktok"},
	{"re recorded", differentRecording, "re recorded|rerecorded|new recording"},
	{"demo", differentRecording, "demo"},
	{"rehearsal", differentRecording, "rehearsal"},
	{"outtake", differentRecording, "outtake"},
	{"session", differentRecording, "session"},
	{"alternate take", differentRecording, "alternate take"},
	{"work tape", differentRecording, "work tape"},
	{"early version", differentRecording, "early version"},
	{"extended", differentRecording, "extended"},
	{"duration", differentRecording | countedDuration, "minute|minutes|hour|hours"},
	{"loop", differentRecording, "loop"},
	{"tv size", differentRecording, "tv size"},
	{"mashup", differentRecording, "mashup"},
	{"medley", differentRecording, "medley"},
	{"megamix", differentRecording, "megamix"},
	{"reprise", differentRecording, "reprise"},
	{"interlude", differentRecording, "interlude"},
	{"intro", differentRecording | exactVersion, "intro"},
	{"outro", differentRecording | exactVersion, "outro"},
	{"skit", differentRecording, "skit"},
	{"part", differentRecording | countedPart | exactVersion, "part|pt"},
	{"version", differentRecording | bareVersion, "version"},
	{"remaster", sameRecording, "remaster|remastered|re master|re mastered"},
	{"release", sameRecording, "explicit version|explicit|deluxe|bonus track|bonus|soundtrack|official audio|lyrics|original mix|original version"},
	{"source", sameRecording | sourceRelease, "from|theme from"},
	{"feature", sameRecording | featureCredit, "featuring|feat|ft|with"},
	{"radio", fallbackEdition, "radio edit|radio|radio version"},
	{"single", fallbackEdition, "single edit|single version"},
	{"album edit", fallbackEdition, "album edit|album version"},
	{"video edit", fallbackEdition, "video edit"},
	{"mono", fallbackEdition, "mono version|mono"},
	{"stereo", fallbackEdition, "stereo version|stereo"},
	{"clean", fallbackEdition, "clean edit|clean version|clean"},
	{"taylors version", naturalRerecord, "taylors version"},
}

type recordingForm struct {
	term  recordingTerm
	words matchPhrase
}

type recordingIndex struct {
	exact map[string][]recordingForm
	cjk   []recordingForm
	fuzzy map[int][]recordingForm
}

var recordingTerms = buildRecordingIndex()

func buildRecordingIndex() recordingIndex {
	index := recordingIndex{exact: make(map[string][]recordingForm), fuzzy: make(map[int][]recordingForm)}
	for _, term := range recordingVocabulary {
		for form := range strings.SplitSeq(term.forms, "|") {
			index.add(recordingForm{term: term, words: matchWords(form)})
		}
	}
	return index
}

func (index *recordingIndex) add(form recordingForm) {
	first := form.words[0]
	if form.fuzzyEnabled() {
		length := utf8.RuneCountInString(first)
		index.fuzzy[length] = append(index.fuzzy[length], form)
	}
	index.exact[first] = append(index.exact[first], form)
	if strings.ContainsFunc(first, cjkRune) {
		index.cjk = append(index.cjk, form)
	}
}

func (f recordingForm) fuzzyEnabled() bool {
	return f.term.roles&exactVersion == 0 && utf8.RuneCountInString(f.words[0]) >= 5
}

func recordingRolePattern(role recordingRole) string {
	var forms []string
	for _, term := range recordingVocabulary {
		if term.roles&role == 0 {
			continue
		}
		for spelling := range strings.SplitSeq(term.forms, "|") {
			forms = append(forms, regexp.QuoteMeta(spelling)+`\.?`)
		}
	}
	return "(?:" + strings.Join(forms, "|") + ")"
}

func cjkRune(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana)
}

func (index recordingIndex) at(words matchPhrase) recordingForm {
	var longest recordingForm
	for _, form := range index.exact[words[0]] {
		if form.prefixOf(words) && len(form.words) > len(longest.words) {
			longest = form
		}
	}
	if longest.words != nil || !strings.ContainsFunc(words[0], cjkRune) {
		return longest
	}
	return index.cjkForm(words[0])
}

func (index recordingIndex) cjkForm(word string) recordingForm {
	for _, form := range index.cjk {
		if strings.Contains(word, form.words[0]) {
			return form
		}
	}
	return recordingForm{}
}

func (index recordingIndex) otherCJK(word string, first recordingForm) []recordingForm {
	if !strings.ContainsFunc(first.words[0], cjkRune) {
		return nil
	}
	var forms []recordingForm
	for _, form := range index.cjk {
		if form.term.id != first.term.id && strings.Contains(word, form.words[0]) {
			forms = append(forms, form)
		}
	}
	return forms
}

func (f recordingForm) prefixOf(words matchPhrase) bool {
	return len(f.words) <= len(words) && slices.Equal(f.words, words[:len(f.words)])
}

type recordingTag struct {
	form  recordingForm
	at    int
	count int
}

func recordingTags(words matchPhrase) []recordingTag {
	var tags []recordingTag
	for i := 0; i < len(words); {
		form := recordingTerms.at(words[i:])
		if form.words == nil {
			i++
			continue
		}
		tags = append(tags, recordingTag{form: form, at: i})
		for _, extra := range recordingTerms.otherCJK(words[i], form) {
			tags = append(tags, recordingTag{form: extra, at: i})
		}
		i += len(form.words)
	}
	return tags
}

func hasRecordingRole(words matchPhrase, role recordingRole) bool {
	return slices.ContainsFunc(recordingTags(words), func(tag recordingTag) bool {
		return tag.form.term.roles&role != 0
	})
}

func (f recordingForm) wordIdentity(query, metadata string) bool {
	if query == metadata {
		return true
	}
	if utf8.RuneCountInString(query) < 6 || utf8.RuneCountInString(metadata) < 5 {
		return false
	}
	pair := runePair{[]rune(query), []rune(metadata)}
	if !pair.allowedStart() || f.disagreesOnTempoInitial(pair) {
		return false
	}
	_, ok := pair.spellingIdentity()
	return ok
}

func (f recordingForm) disagreesOnTempoInitial(pair runePair) bool {
	return f.term.roles&tempoChange != 0 && pair.a[0] != pair.b[0]
}

func (f recordingForm) matches(words matchPhrase) bool {
	if len(f.words) > len(words) {
		return false
	}
	for i, word := range f.words {
		if !f.wordIdentity(words[i], word) {
			return false
		}
	}
	return true
}

const requestedRecordingRoles = differentRecording | fallbackEdition | naturalRerecord

func versionPositions(words matchPhrase) []int {
	var positions []int
	for i := range words {
		if isVersionMarker(words[i:]) {
			positions = append(positions, i)
		}
	}
	return positions
}

func isVersionMarker(words matchPhrase) bool {
	form := recordingTerms.at(words)
	if form.words == nil {
		form = requestedForm(words, requestedRecordingRoles)
	}
	return form.term.roles&requestedRecordingRoles != 0
}

func requestedForm(words matchPhrase, roles recordingRole) recordingForm {
	length := utf8.RuneCountInString(words[0])
	if length < 6 {
		return recordingForm{}
	}
	for _, n := range []int{length, length - 1, length + 1, length - 2, length + 2} {
		if form := fuzzyForm(words, roles, n); form.words != nil {
			return form
		}
	}
	return recordingForm{}
}

func fuzzyForm(words matchPhrase, roles recordingRole, length int) recordingForm {
	for _, form := range recordingTerms.fuzzy[length] {
		if form.term.roles&roles != 0 && form.matches(words) {
			return form
		}
	}
	return recordingForm{}
}
