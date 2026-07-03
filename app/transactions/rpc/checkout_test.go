package rpc

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeGiftMessage(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trims", "  hi there  ", "hi there"},
		{"keeps newlines", "line1\nline2", "line1\nline2"},
		{"tabs become spaces", "a\tb", "a b"},
		{"strips control chars", "hi\x00\x07 there", "hi there"},
		{"empty stays empty", "   ", ""},
	}
	for _, tc := range tests {
		if got := sanitizeGiftMessage(tc.in); got != tc.want {
			t.Errorf("%s: sanitizeGiftMessage(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestSanitizeGiftMessageCaps(t *testing.T) {
	long := strings.Repeat("é", 400) // multi-byte runes to prove the cap counts runes, not bytes
	got := sanitizeGiftMessage(long)
	if n := utf8.RuneCountInString(got); n > giftMessageMaxRunes {
		t.Errorf("capped length = %d runes, want <= %d", n, giftMessageMaxRunes)
	}
}

func TestClampLogin(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"short passes", "bagelfan", "bagelfan"},
		{"trims", "  bagelfan  ", "bagelfan"},
		{"at max", strings.Repeat("a", twitchLoginMaxLen), strings.Repeat("a", twitchLoginMaxLen)},
		{"over max truncates", strings.Repeat("a", 100), strings.Repeat("a", twitchLoginMaxLen)},
	}
	for _, tc := range tests {
		if got := clampLogin(tc.in); got != tc.want {
			t.Errorf("%s: clampLogin(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// The checkout gate rejects a gift note only after sanitizing, so a link
// smuggled through control chars or spacing must survive sanitization and still
// be caught. This proves the sanitize -> ContainsLink pairing the RPC relies on.
func TestGiftNoteLinkAfterSanitize(t *testing.T) {
	blocked := []string{
		"visit example.com now",
		"go to example . com",
		"hey\x00example[.]com",
		"ping me user (at) gmail dot com",
	}
	for _, in := range blocked {
		if !noteHasLink(sanitizeGiftMessage(in)) {
			t.Errorf("gift note %q should be rejected as a link", in)
		}
	}
	clean := []string{"thanks so much, enjoy premium!", "see you at 3 p.m."}
	for _, in := range clean {
		if noteHasLink(sanitizeGiftMessage(in)) {
			t.Errorf("gift note %q should be allowed", in)
		}
	}
}
