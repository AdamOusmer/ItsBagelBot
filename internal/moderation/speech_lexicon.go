// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"embed"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

//go:embed artifact/speech/*.txt
var speechFiles embed.FS

const (
	joinedMark       = "*"
	joinedMinLetters = 5
	maskMinRunes     = 3
)

var speechFloorFiles = []struct {
	reason   SpeechReason
	file     string
	phonetic bool
}{
	{SpeechFloorHate, "hate.txt", true},
	{SpeechFloorSelfHarm, "selfharm.txt", false},
	{SpeechFloorSexual, "sexual.txt", false},
	{SpeechFloorThreat, "threat.txt", false},
}

type speechTerm struct {
	text   string
	joined bool
}

type speechCategory struct {
	reason   SpeechReason
	plain    *matcher
	phonetic *matcher
	joined   *matcher
}

type maskTerm struct {
	runes  []rune
	reason SpeechReason
}

type speechLexicon struct {
	cats  []speechCategory
	masks map[int][]maskTerm
}

var speechFloor = sync.OnceValue(func() *speechLexicon {
	lex := &speechLexicon{masks: map[int][]maskTerm{}}
	for _, spec := range speechFloorFiles {
		data, err := speechFiles.ReadFile("artifact/speech/" + spec.file)
		if err != nil {
			panic(err)
		}
		terms := parseSpeechTerms(data)
		lex.cats = append(lex.cats, buildSpeechCategory(spec.reason, spec.phonetic, terms))
		lex.addMasks(spec.reason, terms)
	}
	return lex
})

func parseSpeechTerms(data []byte) []speechTerm {
	lines := parseTerms(data)
	terms := make([]speechTerm, 0, len(lines))
	for _, line := range lines {
		terms = append(terms, speechTerm{
			text:   strings.TrimPrefix(line, joinedMark),
			joined: strings.HasPrefix(line, joinedMark),
		})
	}
	return terms
}

func isNotLetter(r rune) bool { return !unicode.IsLetter(r) }

func speechKey(text string) string {
	return strings.Join(strings.FieldsFunc(string(Normalize(nil, text)), isNotLetter), " ")
}

func joinable(key string) bool {
	return !strings.Contains(key, " ") && utf8.RuneCountInString(key) >= joinedMinLetters
}

func buildSpeechCategory(reason SpeechReason, phonetic bool, terms []speechTerm) speechCategory {
	var plain, phon, joined [][]byte
	for _, t := range terms {
		key := speechKey(t.text)
		if key == "" {
			continue
		}
		plain = append(plain, []byte(" "+key+" "))
		if phonetic {
			phon = append(phon, []byte(" "+phoneticKey(key)+" "))
		}
		if t.joined && joinable(key) {
			joined = append(joined, []byte(key))
		}
	}
	return speechCategory{reason: reason, plain: matcherOf(plain), phonetic: matcherOf(phon), joined: matcherOf(joined)}
}

func matcherOf(patterns [][]byte) *matcher {
	if len(patterns) == 0 {
		return nil
	}
	return newMatcher(patterns)
}

func (m *matcher) hit(text []byte) bool {
	return m != nil && m.find(text) != noPattern
}

func (l *speechLexicon) addMasks(reason SpeechReason, terms []speechTerm) {
	for _, t := range terms {
		word := strings.ToLower(norm.NFKC.String(t.text))
		runes := []rune(word)
		if len(runes) < maskMinRunes || strings.IndexFunc(word, isNotLetter) >= 0 {
			continue
		}
		l.masks[len(runes)] = append(l.masks[len(runes)], maskTerm{runes: runes, reason: reason})
	}
}

func (l *speechLexicon) scan(variants []foldedText) (SpeechReason, bool) {
	for _, c := range l.cats {
		if c.matches(variants) {
			return c.reason, true
		}
	}
	return SpeechPass, false
}

func (c speechCategory) matches(variants []foldedText) bool {
	for _, v := range variants {
		if c.matchesVariant(v) {
			return true
		}
	}
	return false
}

func (c speechCategory) matchesVariant(v foldedText) bool {
	if c.plain.hit(v.padded) || c.phonetic.hit(v.phonetic) {
		return true
	}
	return c.joined.hit(v.joined)
}
