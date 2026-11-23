// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

type speechSubject struct {
	field    SpeechField
	text     string
	variants [][]byte
}

func newSpeechSubject(field SpeechField, text string) speechSubject {
	return speechSubject{field: field, text: text, variants: foldVariants(text)}
}

func (s speechSubject) floor() SpeechReason {
	reason, _ := speechFloor().scan(s.variants)
	return reason
}
