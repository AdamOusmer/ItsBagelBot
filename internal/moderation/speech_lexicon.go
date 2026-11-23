// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"embed"
	"strings"
	"sync"
	"unicode"
)

//go:embed artifact/speech/*.txt
var speechFiles embed.FS

var speechFloorFiles = []struct {
	reason SpeechReason
	file   string
}{
	{SpeechFloorHate, "hate.txt"},
	{SpeechFloorSelfHarm, "selfharm.txt"},
	{SpeechFloorSexual, "sexual.txt"},
	{SpeechFloorThreat, "threat.txt"},
}

type speechCategory struct {
	reason SpeechReason
	plain  *matcher
}

type speechLexicon struct {
	cats []speechCategory
}

var speechFloor = sync.OnceValue(func() *speechLexicon {
	lex := &speechLexicon{}
	for _, spec := range speechFloorFiles {
		data, err := speechFiles.ReadFile("artifact/speech/" + spec.file)
		if err != nil {
			panic(err)
		}
		lex.cats = append(lex.cats, buildSpeechCategory(spec.reason, parseTerms(data)))
	}
	return lex
})

func isNotLetter(r rune) bool { return !unicode.IsLetter(r) }

func speechKey(text string) string {
	return strings.Join(strings.FieldsFunc(string(Normalize(nil, text)), isNotLetter), " ")
}

func buildSpeechCategory(reason SpeechReason, terms []string) speechCategory {
	var plain [][]byte
	for _, term := range terms {
		if key := speechKey(term); key != "" {
			plain = append(plain, []byte(" "+key+" "))
		}
	}
	return speechCategory{reason: reason, plain: matcherOf(plain)}
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

func (l *speechLexicon) scan(variants [][]byte) (SpeechReason, bool) {
	for _, c := range l.cats {
		if c.matches(variants) {
			return c.reason, true
		}
	}
	return SpeechPass, false
}

func (c speechCategory) matches(variants [][]byte) bool {
	for _, v := range variants {
		if c.plain.hit(v) {
			return true
		}
	}
	return false
}
