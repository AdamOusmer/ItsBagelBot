// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"bytes"
	"testing"
	"unicode"
	"unicode/utf8"
)

// FuzzNormalize pins the documented Normalize contract on arbitrary input:
// lowercase output, no invisible/control/combining runes survive, the only
// whitespace is a single collapsed ' ', and the transform is idempotent (the
// skeleton of a skeleton is itself). Seeds cover the evasion shapes the
// skeleton exists to fold: NFD/NFC mixes, ZWJ emoji families, Cyrillic
// homoglyphs, RTL overrides, quorum-gated leet.
func FuzzNormalize(f *testing.F) {
	seeds := []string{
		"",
		"hello world",
		"GRABIFY.LINK/X",
		"free nitro",
		"FREE,NITRO!!",
		"h4te s3xual n0t",
		"1080 1337 <3",
		"gr\u0430bify \u0435vil",         // Cyrillic а е homoglyphs
		"\u03b1\u03b2\u03b3",             // Greek α β γ
		"\uff28\uff45\uff4c\uff4c\uff4f", // fullwidth HELLO
		"e\u0301xpose\u0301",             // NFD combining acute
		"\u00e9\u00e9 NFC mix",           // precomposed é
		"\U0001f468\u200d\U0001f469\u200d\U0001f466 fam", // ZWJ family emoji
		"\u202eright\u202dto\u202dleft",                  // RTL/LTR overrides
		"a\tb\nc\r\rd  e   f",
		"\ufeff\ufeff bom",
		"\u0130\u00a0nbsp",             // dotted capital I, NBSP
		"\ufb00 ligature \u2460\u2461", // ffi ligature, circled digits
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Add(string([]byte{0xff, 0xfe, 0xfd})) // invalid UTF-8 bytes

	f.Fuzz(func(t *testing.T, text string) {
		out := Normalize(nil, text)

		if !utf8.Valid(out) {
			t.Fatalf("Normalize(%q) emitted invalid UTF-8: %q", text, out)
		}
		for i := 0; i < len(out); i++ {
			if out[i] >= 'A' && out[i] <= 'Z' {
				t.Fatalf("Normalize(%q) = %q: uppercase survives", text, out)
			}
			if i > 0 && out[i] == ' ' && out[i-1] == ' ' {
				t.Fatalf("Normalize(%q) = %q: whitespace run not collapsed", text, out)
			}
		}
		for _, r := range string(out) {
			if unicode.IsSpace(r) && r != ' ' {
				t.Fatalf("Normalize(%q) = %q: non-space whitespace %U survives", text, out, r)
			}
			if r != ' ' && (isStrippable(r) || r < 0x20 || r == 0x7f) {
				t.Fatalf("Normalize(%q) = %q: strippable rune %U survives", text, out, r)
			}
		}

		again := Normalize(nil, string(out))
		if !bytes.Equal(again, out) {
			t.Fatalf("Normalize not idempotent: in=%q once=%q twice=%q", text, out, again)
		}
	})
}
