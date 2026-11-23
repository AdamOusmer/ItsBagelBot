// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func isMaskRune(r rune) bool { return r == '*' || r == '#' }

func isNotLetterOrMask(r rune) bool { return !unicode.IsLetter(r) && !isMaskRune(r) }

func (l *speechLexicon) maskHit(text string) (SpeechReason, bool) {
	literal := strings.ToLower(norm.NFKC.String(text))
	for _, token := range strings.Fields(literal) {
		if reason, ok := l.maskToken(token); ok {
			return reason, true
		}
	}
	return SpeechPass, false
}

func (l *speechLexicon) maskToken(token string) (SpeechReason, bool) {
	runes := []rune(strings.TrimFunc(token, isNotLetterOrMask))
	if !validMask(runes) {
		return SpeechPass, false
	}
	for _, t := range l.masks[len(runes)] {
		if t.agrees(runes) {
			return t.reason, true
		}
	}
	return SpeechPass, false
}

func validMask(runes []rune) bool {
	letters, masks := 0, 0
	for _, r := range runes {
		switch {
		case isMaskRune(r):
			masks++
		case unicode.IsLetter(r):
			letters++
		default:
			return false
		}
	}
	return masks > 0 && letters >= 2
}

func (t maskTerm) agrees(runes []rune) bool {
	for i, r := range t.runes {
		if runes[i] != r && !isMaskRune(runes[i]) {
			return false
		}
	}
	return true
}
