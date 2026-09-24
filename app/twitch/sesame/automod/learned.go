// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"strings"
	"unicode"

	"ItsBagelBot/internal/moderation"
)

func (g *Gate) observeLearned(ch uint64, sender, text string, sig signals) {
	if ch == 0 {
		return
	}
	bl := g.baseline.Load()
	b := g.extra.Load()
	if bl == nil && b == nil {
		return
	}
	toks := strings.Fields(text)
	if bl != nil {
		bl.Observe(ch, sig.capsRatio(), sig.symbolRatio(), float64(len(toks)))
	}
	if b != nil && b.observer != nil {
		b.observer.Observe(ch, sender, toks)
	}
}

func (g *Gate) stripLearned(sig signals, text string, ch uint64) (signals, bool) {
	if ch == 0 {
		return sig, false
	}
	b := g.extra.Load()
	if b == nil || b.set == nil {
		return sig, false
	}
	runes := []rune(text)
	out := sig
	stripped := false
	for i := 0; i < len(runes); {
		end := tokenSpanEnd(runes, i)
		if end > i && b.set.Known(ch, string(runes[i:end])) {
			stripped = true
			subtractTokenEvidence(&out, runes[i:end])
		}
		i = end + 1
	}
	return out, stripped
}

func tokenSpanEnd(runes []rune, i int) int {
	for i < len(runes) && !unicode.IsSpace(runes[i]) {
		i++
	}
	return i
}

func subtractTokenEvidence(out *signals, token []rune) {
	for _, r := range token {
		switch {
		case unicode.IsLetter(r):
			out.letters--
			if unicode.IsUpper(r) {
				out.upper--
			}
		case isEmojiRune(r):
			if r != zeroWidthJoiner {
				out.symbols--
			}
		case moderation.IsInvisible(r):
		case !unicode.IsDigit(r):
			out.symbols--
		}
	}
}

func (g *Gate) purgeLearned(ch uint64, text string) {
	if ch == 0 {
		return
	}
	b := g.extra.Load()
	if b == nil || b.purger == nil {
		return
	}
	b.purger.PurgeTokens(ch, strings.Fields(text))
}
