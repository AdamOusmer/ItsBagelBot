// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"slices"
	"strings"
	"testing"
)

func collectTokens(t *testing.T, text string) []string {
	t.Helper()
	var out []string
	iterLinkTokens(text, func(tok string) { out = append(out, tok) })
	return out
}

func TestIterLinkTokens(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"bare url", "join https://grabify.link/x now", []string{"grabify.link/x"}},
		{"scheme-less", "check sub.grabify.link/x for the event", []string{"sub.grabify.link/x"}},
		{"trailing punctuation", "it's on bit.ly/abc.", []string{"bit.ly/abc"}},
		{"stacked punctuation", "see evil.example!!", []string{"evil.example"}},
		{"caps scheme", "HTTPS://Evil.Example/p now", []string{"Evil.Example/p"}},
		{"port", "evil.com:8080/path ok", []string{"evil.com:8080/path"}},
		// Userinfo survives in the token (hostOf strips it later); the scanner
		// is not a parser and does not pre-interpret @.
		{"userinfo", "phish at https://user@evil.example/p today", []string{"user@evil.example/p"}},
		{"query kept", "open a.example/q?x=1 please", []string{"a.example/q?x=1"}},
		{"ellipsis noise", "wait... what...", nil},
		{"no dots", "no links here friends", nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := collectTokens(t, tt.text); !slices.Equal(got, tt.want) {
				t.Fatalf("tokens %q = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestHostOfAndValidHost(t *testing.T) {
	cases := []struct {
		token  string
		host   string
		valid  bool
		folded string
	}{
		{token: "grabify.link/x", host: "grabify.link", valid: true, folded: "grabify.link"},
		{token: "SUB.Evil.Example", host: "sub.evil.example", valid: true, folded: "evil.example"},
		{token: "foo.co.uk/a", host: "foo.co.uk", valid: true, folded: "foo.co.uk"},
		{token: "deep.a.b.evil.example", host: "deep.a.b.evil.example", valid: true, folded: "evil.example"},
		{token: "xn--80ak6aa92e.tk", host: "xn--80ak6aa92e.tk", valid: true, folded: "xn--80ak6aa92e.tk"}, //gitleaks:allow punycode host fixture, not a secret
		{token: "10.0.0.1", host: "10.0.0.1", valid: false},
		{token: "notahost", host: "notahost", valid: false},
		{token: "a.b", host: "a.b", valid: false},
	}
	for _, tt := range cases {
		// Production lowercases before classification (buildTask), so the
		// table runs on the lowered form; hostOf itself preserves case.
		host := strings.ToLower(hostOf(tt.token))
		if host != tt.host {
			t.Errorf("hostOf(%q) = %q, want %q", tt.token, host, tt.host)
		}
		if valid := validHost(strings.ToLower(host)); valid != tt.valid {
			t.Errorf("validHost(%q) = %v, want %v", host, valid, tt.valid)
		}
		if !tt.valid {
			continue
		}
		if f := foldHost(host); f != tt.folded {
			t.Errorf("foldHost(%q) = %q, want %q", host, f, tt.folded)
		}
	}
}

func TestValidHostRejectsUnicodeAndPorts(t *testing.T) {
	for _, h := range []string{"пример.рф", "evîl.example", "evil.example:8080"} {
		if validHost(h) {
			t.Errorf("validHost(%q) = true, want false", h)
		}
	}
}

func TestTrimLinkTokenDropsSchemeOnly(t *testing.T) {
	// A bare TLD-ish token with no second label never becomes a candidate:
	// "..." must not survive as "." after trimming.
	if got := trimLinkToken("..."); got != "" {
		t.Fatalf("trimLinkToken(...) = %q, want empty", got)
	}
}
