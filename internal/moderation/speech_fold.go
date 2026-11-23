// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	speechSeparators = ".-_*,/|\\+':’"
	singleRunMin     = 3
	shortTokenMax    = 3
	repeatKeep       = 2
)

type speechToken string

type speechTokens []speechToken

type skeleton []rune

type separatorMode bool

type tokenMapper func(speechToken) speechToken

const (
	keepSeparators separatorMode = false
	dropSeparators separatorMode = true
)

var phoneticTable = []string{"ph", "f", "ck", "k", "q", "k", "x", "ks"}

var phoneticReplacer = strings.NewReplacer(phoneticTable...)

var innocentCollisions = map[speechToken]bool{"niger": true, "fagot": true}

type foldedText struct {
	padded   []byte
	phonetic []byte
	joined   []byte
}

func foldVariants(text string) []foldedText {
	skel := skeleton(string(Normalize(nil, text)))
	variants := append(foldPair(skel, dropSeparators), foldPair(skel, keepSeparators)...)
	return uniqueFolded(variants)
}

func foldPair(skel skeleton, mode separatorMode) []foldedText {
	tokens := skel.tokens(mode).joinSingleRuns()
	collapsed := tokens.mapped(collapseRepeats)
	reduced := collapsed.mapped(reduceRepeats)
	return []foldedText{newFolded(collapsed), newFolded(reduced)}
}

func uniqueFolded(list []foldedText) []foldedText {
	seen := make(map[string]bool, len(list))
	out := list[:0]
	for _, f := range list {
		key := string(f.padded)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

func newFolded(tokens speechTokens) foldedText {
	text := tokens.text()
	return foldedText{
		padded:   []byte(" " + text + " "),
		phonetic: []byte(" " + phoneticKey(text) + " "),
		joined:   tokens.joinedRuns(),
	}
}

func phoneticKey(key string) string {
	return fieldTokens(key).mapped(phoneticWord).text()
}

func phoneticWord(word speechToken) speechToken {
	replaced := phoneticReplacer.Replace(string(word))
	if strings.HasSuffix(replaced, "z") {
		return speechToken(strings.TrimSuffix(replaced, "z") + "s")
	}
	return speechToken(replaced)
}

func fieldTokens(text string) speechTokens {
	fields := strings.Fields(text)
	out := make(speechTokens, len(fields))
	for i, f := range fields {
		out[i] = speechToken(f)
	}
	return out
}

func (t speechTokens) plain() []string {
	out := make([]string, len(t))
	for i, token := range t {
		out[i] = string(token)
	}
	return out
}

func (t speechTokens) text() string { return strings.Join(t.plain(), " ") }

func (t speechTokens) concat() string { return strings.Join(t.plain(), "") }

func (t speechTokens) mapped(fn tokenMapper) speechTokens {
	out := make(speechTokens, len(t))
	for i, token := range t {
		out[i] = fn(token)
	}
	return out
}

func (t speechToken) runeCount() int { return utf8.RuneCountInString(string(t)) }

func collapseRepeats(token speechToken) speechToken { return limitRuns(token, repeatKeep) }

func reduceRepeats(token speechToken) speechToken {
	reduced := limitRuns(token, 1)
	if innocentCollisions[reduced] {
		return token
	}
	return reduced
}

func limitRuns(token speechToken, keep int) speechToken {
	var b strings.Builder
	var prev rune
	run := 0
	for _, r := range string(token) {
		if r == prev {
			run++
		} else {
			prev, run = r, 1
		}
		if run <= keep {
			b.WriteRune(r)
		}
	}
	return speechToken(b.String())
}

func isSpeechSeparator(r rune) bool { return strings.ContainsRune(speechSeparators, r) }

func (s skeleton) letterAt(i int) bool {
	return i >= 0 && i < len(s) && unicode.IsLetter(s[i])
}

func (s skeleton) separatorRunEnd(i int) int {
	for i < len(s) && isSpeechSeparator(s[i]) {
		i++
	}
	return i
}

func (s skeleton) bridgedSeparators() []bool {
	flags := make([]bool, len(s))
	for i := 0; i < len(s); {
		end := s.separatorRunEnd(i)
		s.markBridge(flags, i, end)
		i = max(end, i+1)
	}
	return flags
}

func (s skeleton) markBridge(flags []bool, start, end int) {
	if end == start {
		return
	}
	if !s.letterAt(start-1) || !s.letterAt(end) {
		return
	}
	for k := start; k < end; k++ {
		flags[k] = true
	}
}

func (s skeleton) tokens(mode separatorMode) speechTokens {
	bridged := s.bridgedSeparators()
	out := make([]rune, 0, len(s))
	for i, r := range s {
		switch {
		case unicode.IsLetter(r):
			out = append(out, r)
		case bool(mode) && bridged[i]:
		default:
			out = append(out, ' ')
		}
	}
	return fieldTokens(string(out))
}

func (t speechTokens) singleRunEnd(i int) int {
	for i < len(t) && t[i].runeCount() == 1 {
		i++
	}
	return i
}

func (t speechTokens) joinSingleRuns() speechTokens {
	out := make(speechTokens, 0, len(t))
	for i := 0; i < len(t); {
		end := t.singleRunEnd(i)
		if end-i >= singleRunMin {
			out = append(out, speechToken(t[i:end].concat()))
			i = end
			continue
		}
		out = append(out, t[i])
		i++
	}
	return out
}

func (t speechTokens) shortRunEnd(i int) int {
	for i < len(t) && t[i].runeCount() <= shortTokenMax {
		i++
	}
	return i
}

func (t speechTokens) joinedRuns() []byte {
	var runs speechTokens
	for i := 0; i < len(t); {
		end := t.shortRunEnd(i)
		if end-i >= 2 {
			runs = append(runs, speechToken(t[i:end].concat()))
		}
		i = max(end, i+1)
	}
	return []byte(runs.text())
}
