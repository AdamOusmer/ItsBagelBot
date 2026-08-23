// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"strings"
	"unicode"

	"ItsBagelBot/internal/moderation"
)

// The gate-side wiring of the learned layers (baseline.go, vocab.go): one
// observation chokepoint inside Assess, one evidence-suppression pass, and the
// purge-on-strike hook. Everything here is inert unless a provider was wired
// (SetBaseline / SetExtraEmotes) AND the caller scoped the line with
// WithChannel(ch != 0), which is what keeps every pre-existing call site and
// test byte-identical.

// observeLearned folds one judged line into both learned layers, before any
// verdict resolves so hype-channel chatter feeds the models regardless of
// outcome. Baseline gets the scan-derived ratios and token count; Vocab gets
// the raw-text whitespace tokens. A wired channel pays one strings.Fields
// slice allocation per judged line - the documented zero-alloc guarantee
// covers UNWIRED clean paths (TestInspectCleanShortIsNoneAndZeroAlloc runs
// without WithChannel), and trading it for per-channel adaptation on wired
// production traffic is deliberate.
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

// stripLearned reports whether any of text's whitespace tokens is LEARNED for
// channel ch, and if so returns signals minus the style evidence those tokens
// contributed (subtractTokenEvidence). Runs only after a flag already fired on
// the full view.
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
		i = end + 1 // step over the separating space
	}
	return out, stripped
}

// tokenSpanEnd returns the index of the first whitespace rune at or after i -
// the exclusive end of the whitespace-token starting there.
func tokenSpanEnd(runes []rune, i int) int {
	for i < len(runes) && !unicode.IsSpace(runes[i]) {
		i++
	}
	return i
}

// subtractTokenEvidence removes one Known token's style evidence from out,
// mirroring scan()'s classification per rune. Only letters/upper/symbols are
// subtracted - exactly the inputs of the caps and symbol comparisons.
// Deliberately NOT subtracted:
//
//   - zeroWidth and repeat runs: evasion signals, never communal style;
//   - runes/spaces/emoji: they back emojiDominant's denominator and the caps
//     min-length precondition, and corrupting them to chase a slightly lower
//     symbolRatio risks flipping the emoji-hype rescue's arithmetic.
//
// Subtraction can only remove evidence for tokens that ARE known; an unknown
// token's shouting keeps counting fully. Runs only after a flag already fired
// on the full view, so the recompute can drop a flag but never mint one.
func subtractTokenEvidence(out *signals, token []rune) {
	for _, r := range token {
		switch {
		case unicode.IsLetter(r):
			out.letters--
			if unicode.IsUpper(r) {
				out.upper--
			}
		case isEmojiRune(r):
			if r != 0x200d { // ZWJ mints no symbol, mirroring scan()
				out.symbols--
			}
		case moderation.IsInvisible(r):
			// invisible runes are zeroWidth evidence: never stripped
		case !unicode.IsDigit(r):
			out.symbols--
		}
	}
}

// purgeLearned forgets every token of this message for channel ch - called
// ONLY when a lexicon/hate-floor/block-term verdict lands. Enforcement contact
// whitelists nothing: a token that rode a slur through the learner loses its
// tau x d progress and its sender set, so laundering resets to zero on first
// strike instead of minting a Known slur-adjacent word.
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
