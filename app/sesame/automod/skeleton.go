package automod

import (
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// isInvisible reports the explicit zero-width and RTL/LTR-override code points
// spam uses to break up tokens or defeat dedup. Counted as a signal by scan.
// Written as hex code points so no invisible rune ever sits in the source.
func isInvisible(r rune) bool {
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
	return isInvisible(r) || unicode.IsControl(r) ||
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
	// Digit/symbol leet (scoped to skeleton blocklist matching).
	'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '8': 'b', '@': 'a', '$': 's',
}

// Normalize folds a message into its detection skeleton and writes it into dst (a
// pooled buffer), returning the slice: NFKC (fullwidth/compat to ascii), strip
// invisible/control/combining, fold confusable lookalikes to latin, lowercase,
// collapse whitespace runs. It runs only on the flagged path, so its allocations
// never touch the clean hot path.
func Normalize(dst []byte, text string) []byte {
	dst = dst[:0]
	var buf [utf8.UTFMax]byte
	nf := norm.NFKC.AppendString(nil, text)
	spaced := false
	for i := 0; i < len(nf); {
		r, size := utf8.DecodeRune(nf[i:])
		i += size
		switch {
		case isStrippable(r):
			continue
		case unicode.IsSpace(r):
			if spaced {
				continue
			}
			spaced = true
			dst = append(dst, ' ')
			continue
		}
		spaced = false
		// Lowercase first, THEN fold: a single lowercase confusables entry then
		// catches an uppercase cross-script lookalike too (uppercase Cyrillic 'А'
		// lowercases to 'а' before the fold), closing an evasion gap.
		r = unicode.ToLower(r)
		if c, ok := confusables[r]; ok {
			r = c
		}
		n := utf8.EncodeRune(buf[:], r)
		dst = append(dst, buf[:n]...)
	}
	return dst
}
