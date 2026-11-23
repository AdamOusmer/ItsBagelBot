// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

type channelSection struct {
	cat     Category
	reason  SpeechReason
	enabled bool
}

func channelSections(p SpeechPolicy) [3]channelSection {
	return [3]channelSection{
		{CatHarassment, SpeechChannelHarassment, p.Harassment},
		{CatSexual, SpeechChannelSexual, p.Sexual},
		{CatProfanity, SpeechChannelProfanity, p.Profanity},
	}
}

func (l *Lexicon) hitsAny(cat Category, variants [][]byte) bool {
	m := l.cats[cat]
	for _, v := range variants {
		if m.hit(v) {
			return true
		}
	}
	return false
}

func (s speechSubject) channel(policy SpeechPolicy) SpeechReason {
	skel := Normalize(nil, s.text)
	if ContainsAnyTerm(skel, policy.AllowTerms) {
		return SpeechPass
	}
	lex := EmbeddedLexicon()
	for _, sec := range channelSections(policy) {
		if sec.enabled && lex.hitsAny(sec.cat, s.variants) {
			return sec.reason
		}
	}
	if ContainsAnyTerm(skel, policy.BlockTerms) {
		return SpeechChannelTerm
	}
	return SpeechPass
}
