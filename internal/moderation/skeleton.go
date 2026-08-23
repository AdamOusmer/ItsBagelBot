// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// IsInvisible reports the explicit zero-width and RTL/LTR-override code points
// spam uses to break up tokens or defeat dedup. Counted as a signal by scan.
// Written as hex code points so no invisible rune ever sits in the source.
func IsInvisible(r rune) bool {
	switch r {
	case 0x200b, // zero width space
		0x200c, // zero width non-joiner
		0x200d, // zero width joiner
		0x2060, // word joiner
		0xfeff, // zero width no-break space / BOM
		0x180e, // Mongolian vowel separator
		0x202e, // right-to-left override
		0x202d: // left-to-right override
		return true
	}
	return false
}

// isStrippable reports code points removed from the skeleton: the invisible set
// above, all format (Cf) and non-spacing-mark (Mn, Zalgo) runes, and controls.
func isStrippable(r rune) bool {
	return IsInvisible(r) || unicode.IsControl(r) ||
		unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r)
}

// confusables folds common cross-script and leet lookalikes to their latin
// skeleton. Keys are the LOWERCASE code point: Normalize lowercases before it
// folds, so a single lowercase entry catches both cases (an uppercase Cyrillic 'А'
// lowercases to 'а' and then folds), and the map stays half the size. A curated
// set (Cyrillic, Greek, math/fullwidth left to NFKC, digit/symbol leet); the full
// Unicode confusables table is loaded from the pattern artifact in a later phase.
var confusables = map[rune]rune{
	// Cyrillic lowercase lookalikes.
	0x0430: 'a', 0x0435: 'e', 0x043e: 'o', 0x0440: 'p', 0x0441: 'c', // а е о р с
	0x0445: 'x', 0x0443: 'y', 0x043a: 'k', 0x043c: 'm', 0x0442: 't', // х у к м т
	0x043d: 'h', 0x0432: 'b', 0x0456: 'i', 0x0455: 's', 0x0458: 'j', // н в і ѕ ј
	0x0501: 'd', 0x04bb: 'h', 0x0433: 'r', // ԁ һ г
	// Greek lowercase lookalikes.
	0x03b1: 'a', 0x03b2: 'b', 0x03b5: 'e', 0x03b7: 'h', 0x03b9: 'i', // α β ε η ι
	0x03ba: 'k', 0x03bd: 'v', 0x03bf: 'o', 0x03c1: 'p', 0x03c4: 't', // κ ν ο ρ τ
	0x03c5: 'y', 0x03c7: 'x', 0x03b6: 'z', 0x03c9: 'w', 0x03c3: 'o', // υ χ ζ ω σ
	// Digit/symbol leet (scoped to skeleton blocklist matching). GATED - see
	// isLeetFold and the quorum in Normalize.
	'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '8': 'b', '@': 'a', '$': 's',
}

// isLeetFold reports whether a confusable key is a digit/symbol whose fold is
// gated behind the two-letter quorum in Normalize. Unconditional folding turned
// any number into letter soup ("1080" -> "ioao", "1337" -> "ieet") that could
// drift onto lexicon terms or wreck dedup fingerprints; letters-only obfuscation
// ("h4te", "n0t") carries its own real letters and keeps folding.
func isLeetFold(r rune) bool {
	switch r {
	case '0', '1', '3', '4', '5', '7', '8', '@', '$':
		return true
	}
	return false
}

// asciiTokenLetters counts the plain ascii letters in the whitespace-token
// containing byte pos (the token begins at tokStart), skipping the gated byte
// itself: a digit never counts toward its own quorum, so "1337" cannot vote
// itself into "leet". Fast path only - bytes are known ascii.
func asciiTokenLetters(text string, tokStart, gatePos int) int {
	letters := 0
	for i := tokStart; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r' {
			break
		}
		if i != gatePos && ('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			letters++
			if letters >= 2 {
				break // early exit bounds the scan; more letters change nothing
			}
		}
	}
	return letters
}

// skelTokenLetters is asciiTokenLetters over the NFKC buffer, where runes are
// multi-byte and lookalike LETTERS (Cyrillic а, Greek α) must count toward the
// quorum so "grаb1fy"-style mixed evasion still folds. The gated rune at
// gatePos is skipped, strippable runes are token-transparent (they vanish from
// the skeleton without ending the token), and gated digits themselves never
// count - "1080" stays "1080".
//
// Transparency must be checked BEFORE the IsSpace break (found by FuzzNormalize,
// 2026-08-22): '\v'/'\f'/NEL are BOTH strippable controls and unicode.IsSpace,
// and the main loop strips them - so "A\f0A"'s skeleton is "a0a", one token.
// Counting the \f as a token end saw one letter and refused to fold the '0',
// making pass one emit "a0a" whose own re-normalization folds to "aoa": the
// skeleton was not idempotent and disagreed with itself between raw and
// normalized forms. Breaking on a rune that will not exist in the skeleton is
// always wrong; boundaries are exactly the runes that survive as separators.
func skelTokenLetters(nf []byte, tokStart, gatePos int) int {
	letters := 0
	for j := tokStart; j < len(nf); {
		if j == gatePos {
			j++
			continue
		}
		r, size := utf8.DecodeRune(nf[j:])
		j += size
		if isStrippable(r) {
			continue // vanishes from the skeleton: transparent inside the token
		}
		if unicode.IsSpace(r) {
			break
		}
		lr := unicode.ToLower(r)
		if f, ok := confusables[lr]; ok {
			if !isLeetFold(lr) {
				lr = f // lookalike letters count toward the quorum...
			} else {
				continue // ...leet digits do not, not even each other
			}
		}
		if lr >= 'a' && lr <= 'z' {
			letters++
			if letters >= 2 {
				break
			}
		}
	}
	return letters
}

// Normalize folds a message into its detection skeleton and writes it into dst (a
// pooled buffer), returning the slice: NFKC (fullwidth/compat to ascii), strip
// invisible/control/combining, fold confusable lookalikes to latin, lowercase,
// collapse whitespace runs. It runs only on the flagged path, so its allocations
// never touch the clean hot path.
//
// Leet digits/symbols ('0','1','3','4','5','7','8','@','$') fold ONLY when
// their containing whitespace-token carries >=2 real ascii letters (lookalike
// letters count): "h4te"/"s3xual"/"n0t" still fold, "1080"/"1337"/"<3" keep
// their digits. Lowercase-before-fold ordering preserved.
//
// Pure-ascii input takes a byte-wise fast path that skips NFKC and rune
// decoding entirely (NFKC is identity on ASCII; the only strippable ASCII
// runes are C0 controls + DEL): an ordinary chat line normalizes alloc-free,
// measured before/after in BenchmarkNormalize's comment.
func Normalize(dst []byte, text string) []byte {
	dst = dst[:0]

	plain := true
	for i := 0; i < len(text); i++ {
		if c := text[i]; c >= utf8.RuneSelf || c < 0x20 || c == 0x7f {
			plain = false
			break
		}
	}
	if plain {
		spaced := false
		tokStart := 0
		for i := 0; i < len(text); i++ {
			c := text[i]
			if c == ' ' {
				if !spaced {
					dst = append(dst, ' ')
				}
				spaced = true
				tokStart = i + 1
				continue
			}
			spaced = false
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if f, ok := confusables[rune(c)]; ok &&
				(!isLeetFold(rune(c)) || asciiTokenLetters(text, tokStart, i) >= 2) {
				c = byte(f)
			}
			dst = append(dst, c)
		}
		return dst
	}

	var buf [utf8.UTFMax]byte
	// Sanitize invalid UTF-8 BEFORE NFKC (found by FuzzNormalize, 2026-08-22):
	// AppendString has streaming semantics and passes a TRAILING incomplete
	// sequence through raw on the assumption more input will complete it - a
	// chat line cut mid-rune kept its tail byte unnormalized, so the skeleton
	// disagreed with its own re-normalization (non-idempotent) and left
	// compatibility letters like U+02B8 unfolded for the word-bounded scans.
	// ToValidUTF8 substitutes FFFD exactly as DecodeRune already does for
	// interior invalid bytes; valid input (the common case) pays one scan and
	// zero allocations.
	if !utf8.ValidString(text) {
		text = toValidUTF8(text)
	}
	nf := norm.NFKC.AppendString(nil, text)
	spaced := false
	tokStart := 0
	for i := 0; i < len(nf); {
		r, size := utf8.DecodeRune(nf[i:])
		i += size
		switch {
		case isStrippable(r):
			continue
		case unicode.IsSpace(r):
			if !spaced {
				dst = append(dst, ' ')
			}
			spaced = true
			tokStart = i
			continue
		}
		spaced = false
		// Lowercase first, THEN fold: a single lowercase confusables entry then
		// catches an uppercase cross-script lookalike too (uppercase Cyrillic 'А'
		// lowercases to 'а' before the fold), closing an evasion gap.
		r = unicode.ToLower(r)
		if f, ok := confusables[r]; ok &&
			(!isLeetFold(r) || skelTokenLetters(nf, tokStart, i-size) >= 2) {
			r = f
		}
		n := utf8.EncodeRune(buf[:], r)
		dst = append(dst, buf[:n]...)
	}
	return dst
}

// toValidUTF8 replaces every invalid byte run in s with U+FFFD. strings.
// ToValidUTF8 does exactly this; the alias keeps the normalization story in
// one file (and lets tests stub nothing).
func toValidUTF8(s string) string { return strings.ToValidUTF8(s, string(rune(0xfffd))) }
