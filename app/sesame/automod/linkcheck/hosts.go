// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package linkcheck is the automod's dynamic link-safety layer: it classifies
// link hosts that chat carries but no static list knows yet, through passive
// oracles only — Cloudflare's 1.1.1.1 for Families security resolver (a blocked
// domain answers 0.0.0.0) and hourly-pulled community blocklists (OpenPhish,
// URLhaus). It never fetches a chat link's destination: confirming liveness to
// attacker infrastructure and driving traffic at scam pages are exactly the
// harms the static floor exists to prevent, so the only outbound requests that
// touch anything outside this package's allowlisted oracle endpoints are
// redirect-header walks against known shortener hosts (unshorten.go), which
// stop the moment a hop leaves the shortener allowlist and never contact the
// destination.
//
// The gate consults the checker synchronously (map reads only) and the checker
// resolves unknown hosts on its own goroutines; verdicts apply to subsequent
// lines carrying the same host. This file split keeps each concern (host shape,
// caching, oracles, expansion) reviewable alone.
package linkcheck

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

// iterLinkTokens walks text's whitespace-delimited tokens and calls fn with
// every token that could carry a link: the authority plus any path, stripped of
// scheme, userinfo and trailing punctuation. Tokens are substrings of text —
// the walk itself never allocates, which is what makes it safe to run from the
// gate's clean path on every line containing a dot (the cheap pre-filter before
// this runs). Chat links arrive bare ("join discord.gg/x"), which url.Parse
// rejects, so this is deliberately a scanner, not a parser.
func iterLinkTokens(text string, fn func(token string)) {
	for i := 0; i < len(text); {
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		start := i
		for i < len(text) && !isSpaceByte(text[i]) {
			i++
		}
		if start == i {
			return
		}
		if tok := trimLinkToken(text[start:i]); tok != "" && strings.Contains(tok, ".") {
			fn(tok)
		}
	}
}

func isSpaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// trimLinkToken normalizes one raw token into candidate link form: scheme off,
// leading www. off, trailing punctuation off. Returns "" when nothing remains.
func trimLinkToken(tok string) string {
	// Scheme strip, case-insensitive: "HTTPS://X" arrives from caps-heavy hype.
	if len(tok) >= 8 && equalFoldASCII(tok[:8], "https://") {
		tok = tok[8:]
	} else if len(tok) >= 7 && equalFoldASCII(tok[:7], "http://") {
		tok = tok[7:]
	}
	// www. prefix is presentation, not identity: bit.ly and www.bit.ly must
	// land on one cache entry.
	if len(tok) >= 4 && equalFoldASCII(tok[:4], "www.") {
		tok = tok[4:]
	}
	// Trailing punctuation is chat prosody, not the link: "bit.ly/x." ends a
	// sentence. A run is trimmed because "...", "!?" stack in real chat. The
	// path may legitimately end in almost anything else; only prose punctuation
	// is released.
	for len(tok) > 0 {
		switch tok[len(tok)-1] {
		case '.', ',', '!', '?', ';', ':', '"', '\'', ')', ']', '>':
			tok = tok[:len(tok)-1]
		default:
			return tok
		}
	}
	return tok
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// hostOf extracts the host component of an iterLinkTokens token: everything
// before the first '/', '?' or '#', minus userinfo and port. The path stays in
// the token itself — shortener destinations differ per path, so expansion keys
// on the full token while plain-host classification keys on the host.
func hostOf(token string) string {
	host := token
	if k := strings.IndexAny(host, "/?#"); k >= 0 {
		host = host[:k]
	}
	if k := strings.LastIndexByte(host, '@'); k >= 0 { // user@host spam shape
		host = host[k+1:]
	}
	if k := strings.LastIndexByte(host, ':'); k >= 0 && isPortSuffix(host[k+1:]) {
		host = host[:k]
	}
	return host
}

// isPortSuffix reports whether s is a plausible port number (digits only).
// Loose by design: ":abc" fails here and stays part of the host, which then
// fails validHost — either way nothing dials it.
func isPortSuffix(s string) bool {
	if len(s) == 0 || len(s) > 5 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// validHost reports whether h has the shape of a DNS name worth classifying:
// ascii letters/digits/hyphens/dots, at least two labels, alphabetic TLD of
// two-plus characters. The TLD rule silently rejects bare IPv4 literals (the
// last label would be numeric) — chat links pointing at raw IPs are rare and
// the floor already covers the abusive-host shapes worth banning on sight.
// Non-ascii (IDN in unicode form) is rejected too: browsers punycode-display
// those behind their own homograph warnings, so the marginal recall is not
// worth feeding confusable hosts to the oracles.
func validHost(h string) bool {
	if len(h) < 4 || len(h) > 253 {
		return false
	}
	labels := 1
	last := byte('.')
	labelStart := 0
	for i := 0; i < len(h); i++ {
		c := h[i]
		switch {
		case c == '.':
			if last == '.' { // empty label ("a..b")
				return false
			}
			labels++
			labelStart = i + 1
		case c == '-' || c == '_' ||
			('a' <= c && c <= 'z') || ('0' <= c && c <= '9'):
			// ok (underscore appears in wildcard-ish junk; harmless to carry)
		default:
			return false // uppercase was already lowered; anything else is out
		}
		last = c
	}
	if labels < 2 || last == '.' {
		return false
	}
	tld := h[labelStart:]
	if len(tld) < 2 || len(tld) > 63 {
		return false
	}
	for i := 0; i < len(tld); i++ {
		if tld[i] < 'a' || tld[i] > 'z' {
			return false
		}
	}
	return true
}

// foldHost reduces a host to its registrable domain (eTLD+1), so sub.domain
// rotation cannot fragment the cache past eviction reach. Errors (single-label
// names, malformed input) fall back to the host verbatim — a cache keyed on
// slightly-wrong granularity still converges, a panic does not.
func foldHost(h string) string {
	folded, err := publicsuffix.EffectiveTLDPlusOne(h)
	if err != nil || folded == "" {
		return h
	}
	return folded
}
