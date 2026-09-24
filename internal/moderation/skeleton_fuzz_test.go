// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"bytes"
	"testing"
	"unicode"
	"unicode/utf8"
)

func fuzzNormalizeSeeds() []string {
	return []string{
		"",
		"hello world",
		"GRABIFY.LINK/X",
		"free nitro",
		"FREE,NITRO!!",
		"h4te s3xual n0t",
		"1080 1337 <3",
		"gr\u0430bify \u0435vil",
		"\u03b1\u03b2\u03b3",
		"\uff28\uff45\uff4c\uff4c\uff4f",
		"e\u0301xpose\u0301",
		"\u00e9\u00e9 NFC mix",
		"\U0001f468\u200d\U0001f469\u200d\U0001f466 fam",
		"\u202eright\u202dto\u202dleft",
		"a\tb\nc\r\rd  e   f",
		"\ufeff\ufeff bom",
		"\u0130\u00a0nbsp",
		"\ufb00 ligature \u2460\u2461",
	}
}

func FuzzNormalize(f *testing.F) {
	for _, s := range fuzzNormalizeSeeds() {
		f.Add(s)
	}
	f.Add(string([]byte{0xff, 0xfe, 0xfd}))

	f.Fuzz(func(t *testing.T, text string) {
		out := Normalize(nil, text)
		assertValidLowerCollapsedUTF8(t, text, out)
		assertNoStrippableSurvivors(t, text, out)
		assertNormalizeIdempotent(t, text, out)
	})
}

func assertValidLowerCollapsedUTF8(t *testing.T, text string, out []byte) {
	t.Helper()
	if !utf8.Valid(out) {
		t.Fatalf("Normalize(%q) emitted invalid UTF-8: %q", text, out)
	}
	assertLowercased(t, text, out)
	assertSingleSpaces(t, text, out)
}

func assertLowercased(t *testing.T, text string, out []byte) {
	t.Helper()
	for i := 0; i < len(out); i++ {
		if out[i] >= 'A' && out[i] <= 'Z' {
			t.Fatalf("Normalize(%q) = %q: uppercase survives", text, out)
		}
	}
}

func assertSingleSpaces(t *testing.T, text string, out []byte) {
	t.Helper()
	for i := 1; i < len(out); i++ {
		if out[i] == ' ' && out[i-1] == ' ' {
			t.Fatalf("Normalize(%q) = %q: whitespace run not collapsed", text, out)
		}
	}
}

func assertNoStrippableSurvivors(t *testing.T, text string, out []byte) {
	t.Helper()
	assertOnlySpaceWhitespace(t, text, out)
	assertNoControlsOrCombining(t, text, out)
}

func assertOnlySpaceWhitespace(t *testing.T, text string, out []byte) {
	t.Helper()
	for _, r := range string(out) {
		if unicode.IsSpace(r) && r != ' ' {
			t.Fatalf("Normalize(%q) = %q: non-space whitespace %U survives", text, out, r)
		}
	}
}

func assertNoControlsOrCombining(t *testing.T, text string, out []byte) {
	t.Helper()
	for _, r := range string(out) {
		if r == ' ' {
			continue
		}
		if isStrippable(r) {
			t.Fatalf("Normalize(%q) = %q: strippable rune %U survives", text, out, r)
		}
		if r < 0x20 || r == 0x7f {
			t.Fatalf("Normalize(%q) = %q: control rune %U survives", text, out, r)
		}
	}
}

func assertNormalizeIdempotent(t *testing.T, text string, out []byte) {
	t.Helper()
	again := Normalize(nil, string(out))
	if !bytes.Equal(again, out) {
		t.Fatalf("Normalize not idempotent: in=%q once=%q twice=%q", text, out, again)
	}
}
