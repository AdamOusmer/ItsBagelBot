// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"unicode"
	"unicode/utf8"
)

func IsInvisible(r rune) bool {
	switch r {
	case 0x200b,
		0x200c,
		0x200d,
		0x2060,
		0xfeff,
		0x180e,
		0x202e,
		0x202d:
		return true
	}
	return false
}

func isStrippable(r rune) bool {
	return IsInvisible(r) || unicode.IsControl(r) ||
		unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r)
}

var confusables = map[rune]rune{
	0x0430: 'a', 0x0435: 'e', 0x043e: 'o', 0x0440: 'p', 0x0441: 'c',
	0x0445: 'x', 0x0443: 'y', 0x043a: 'k', 0x043c: 'm', 0x0442: 't',
	0x043d: 'h', 0x0432: 'b', 0x0456: 'i', 0x0455: 's', 0x0458: 'j',
	0x0501: 'd', 0x04bb: 'h', 0x0433: 'r',
	0x03b1: 'a', 0x03b2: 'b', 0x03b5: 'e', 0x03b7: 'h', 0x03b9: 'i',
	0x03ba: 'k', 0x03bd: 'v', 0x03bf: 'o', 0x03c1: 'p', 0x03c4: 't',
	0x03c5: 'y', 0x03c7: 'x', 0x03b6: 'z', 0x03c9: 'w', 0x03c3: 'o',
}

var leetFolds = map[skelByte]skelByte{
	'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '8': 'b', '@': 'a', '$': 's',
}

type skelByte byte

const lowerDelta = 'a' - 'A'

func (b skelByte) lower() skelByte {
	if 'A' <= b && b <= 'Z' {
		return b + lowerDelta
	}
	return b
}

func isSkelSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\v' || b == '\f' || b == '\r'
}

func printableASCII(b byte) bool {
	return b < utf8.RuneSelf && b >= 0x20 && b != 0x7f
}
