// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"bytes"
	"testing"
	"unicode"
	"unicode/utf8"
)

// fuzzNormalizeSeeds covers the evasion shapes the skeleton exists to fold:
// NFD/NFC mixes, ZWJ emoji families, Cyrillic homoglyphs, RTL overrides,
// quorum-gated leet.
func fuzzNormalizeSeeds() []string {
	return []string{
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
}

// FuzzNormalize pins the documented Normalize contract on arbitrary input:
// lowercase output, no invisible/control/combining runes survive, the only
// whitespace is a single collapsed ' ', and the transform is idempotent (the
// skeleton of a skeleton is itself).
func FuzzNormalize(f *testing.F) {
	for _, s := range fuzzNormalizeSeeds() {
		f.Add(s)
	}
	f.Add(string([]byte{0xff, 0xfe, 0xfd})) // invalid UTF-8 bytes

	f.Fuzz(func(t *testing.T, text string) {
		out := Normalize(nil, text)
		assertValidLowerCollapsedUTF8(t, text, out)
		assertNoStrippableSurvivors(t, text, out)
		assertNormalizeIdempotent(t, text, out)
	})
}

// assertValidLowerCollapsedUTF8 pins the byte-level shape: valid UTF-8, no
// uppercase survivor, whitespace runs collapsed to single spaces.
func assertValidLowerCollapsedUTF8(t *testing.T, text string, out []byte) {
	t.Helper()
	if !utf8.Valid(out) {
		t.Fatalf("Normalize(%q) emitted invalid UTF-8: %q", text, out)
	}
	assertLowercased(t, text, out)
	assertSingleSpaces(t, text, out)
}

// assertLowercased fails when any ASCII uppercase letter survives.
func assertLowercased(t *testing.T, text string, out []byte) {
	t.Helper()
	for i := 0; i < len(out); i++ {
		if out[i] >= 'A' && out[i] <= 'Z' {
			t.Fatalf("Normalize(%q) = %q: uppercase survives", text, out)
		}
	}
}

// assertSingleSpaces fails when two space bytes sit adjacent.
func assertSingleSpaces(t *testing.T, text string, out []byte) {
	t.Helper()
	for i := 1; i < len(out); i++ {
		if out[i] == ' ' && out[i-1] == ' ' {
			t.Fatalf("Normalize(%q) = %q: whitespace run not collapsed", text, out)
		}
	}
}

// assertNoStrippableSurvivors pins the rune-level shape: the only whitespace
// left is ' ', and no invisible/control/combining rune survives.
func assertNoStrippableSurvivors(t *testing.T, text string, out []byte) {
	t.Helper()
	assertOnlySpaceWhitespace(t, text, out)
	assertNoControlsOrCombining(t, text, out)
}

// assertOnlySpaceWhitespace fails when any non-' '-space whitespace survives.
func assertOnlySpaceWhitespace(t *testing.T, text string, out []byte) {
	t.Helper()
	for _, r := range string(out) {
		if unicode.IsSpace(r) && r != ' ' {
			t.Fatalf("Normalize(%q) = %q: non-space whitespace %U survives", text, out, r)
		}
	}
}

// assertNoControlsOrCombining fails when a strippable rune survives.
func assertNoControlsOrCombining(t *testing.T, text string, out []byte) {
	t.Helper()
	for _, r := range string(out) {
		if r != ' ' && (isStrippable(r) || r < 0x20 || r == 0x7f) {
			t.Fatalf("Normalize(%q) = %q: strippable rune %U survives", text, out, r)
		}
	}
}

// assertNormalizeIdempotent pins the skeleton of a skeleton being itself.
func assertNormalizeIdempotent(t *testing.T, text string, out []byte) {
	t.Helper()
	again := Normalize(nil, string(out))
	if !bytes.Equal(again, out) {
		t.Fatalf("Normalize not idempotent: in=%q once=%q twice=%q", text, out, again)
	}
}
