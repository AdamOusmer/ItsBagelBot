// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

type speechHit struct {
	reason   SpeechReason
	maskOnly bool
}

type speechSubject struct {
	field    SpeechField
	text     string
	variants []foldedText
}

func newSpeechSubject(field SpeechField, text string) speechSubject {
	return speechSubject{field: field, text: text, variants: foldVariants(text)}
}

func (s speechSubject) floor() speechHit {
	lex := speechFloor()
	if reason, ok := lex.scan(s.variants); ok {
		return speechHit{reason: reason}
	}
	if reason, ok := lex.maskHit(s.text); ok {
		return speechHit{reason: reason, maskOnly: true}
	}
	return speechHit{}
}
