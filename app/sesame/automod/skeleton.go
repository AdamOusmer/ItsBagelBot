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
// skeleton. A curated starter set (Cyrillic/Greek/digits); the full Unicode
// confusables table is loaded from the pattern artifact in a later phase.
var confusables = map[rune]rune{
	0x0430: 'a', 0x0435: 'e', 0x043e: 'o', 0x0440: 'p', 0x0441: 'c', // а е о р с
	0x0445: 'x', 0x0443: 'y', 0x043a: 'k', 0x043c: 'm', 0x0442: 't', // х у к м т
	0x043d: 'h', 0x0432: 'b', 0x0456: 'i', 0x0455: 's', // н в і ѕ
	0x0391: 'a', 0x0392: 'b', 0x0395: 'e', 0x0397: 'h', 0x0399: 'i', // Α Β Ε Η Ι
	0x039a: 'k', 0x039c: 'm', 0x039d: 'n', 0x039f: 'o', 0x03a1: 'p', // Κ Μ Ν Ο Ρ
	0x03a4: 't', 0x03a5: 'y', 0x03a7: 'x', 0x0396: 'z', // Τ Υ Χ Ζ
	'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '@': 'a', '$': 's',
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
		if c, ok := confusables[r]; ok {
			r = c
		}
		r = unicode.ToLower(r)
		n := utf8.EncodeRune(buf[:], r)
		dst = append(dst, buf[:n]...)
	}
	return dst
}
