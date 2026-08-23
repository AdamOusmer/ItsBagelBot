// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The skeleton character alphabet: the code-point predicates that decide what
// survives Normalize and the two fold tables it applies. Split from the
// normalize pipeline so each file owns one concern.

package moderation

import (
	"unicode"
	"unicode/utf8"
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
}

// leetFolds holds the digit/symbol folds scoped to skeleton blocklist matching.
// Their own table encodes the gate: unlike confusables these fold ONLY behind
// the two-letter quorum in Normalize. Unconditional folding turned any number
// into letter soup ("1080" -> "ioao", "1337" -> "ieet") that could drift onto
// lexicon terms or wreck dedup fingerprints; letters-only obfuscation ("h4te",
// "n0t") carries its own real letters and keeps folding.
var leetFolds = map[skelByte]skelByte{
	'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '8': 'b', '@': 'a', '$': 's',
}

// skelByte is one byte of skeleton space: post-lowercase, post-NFKC material
// the blocklist scanners walk. A named type, because "which alphabet a byte
// belongs to" is exactly the confusion the skeleton exists to prevent.
type skelByte byte

const lowerDelta = 'a' - 'A'

// lower lowercases an ascii-range skeleton byte, passing everything else.
func (b skelByte) lower() skelByte {
	if 'A' <= b && b <= 'Z' {
		return b + lowerDelta
	}
	return b
}

// isSkelSpace reports whether b is a whitespace byte that survives Normalize
// as a token boundary (the skeleton emits a single ' ' for any run of these).
func isSkelSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\v' || b == '\f' || b == '\r'
}

// printableASCII reports whether b survives Normalize's fast path unchanged:
// any non-ASCII byte routes to the unicode scanner, controls and DEL are
// stripped there.
func printableASCII(b byte) bool {
	return b < utf8.RuneSelf && b >= 0x20 && b != 0x7f
}
