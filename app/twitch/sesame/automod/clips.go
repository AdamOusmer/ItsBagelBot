// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import "strings"

// hasNonClipLink reports whether text carries a link-shaped token that is not
// a Twitch clip URL. Used only when the clips_only section is on: clip links
// stay, every other host shape is deleted. Chat links arrive bare
// ("discord.gg/x"), which url.Parse rejects, so this is a whitespace-token
// scanner rather than a parser - same shape as the campaign linkish signal,
// but host-validated so prose ellipses ("wait...") never mint a delete.
//
// Allowed clip forms (scheme and leading www. optional):
//
//	clips.twitch.tv/<slug>
//	twitch.tv/<login>/clip/<slug>
//
// Bare twitch.tv/<login>, clips.twitch.tv with no slug, and every other host
// count as non-clip. Shorteners are never treated as clips: the destination is
// not fetched (same account-safety rule as the IP-logger floor).
func hasNonClipLink(text string) bool {
	for i := 0; i < len(text); {
		start, end := nextSpaceToken(text, i)
		if start == end {
			return false
		}
		if tok := trimLinkToken(text[start:end]); tok != "" && looksLikeLinkHost(tok) && !isTwitchClipToken(tok) {
			return true
		}
		i = end
	}
	return false
}

// maybeLinkText is the cheap pre-filter before forcing the deep path for
// clips_only: a line with no '.' and no http scheme cannot carry a host-shaped
// token, so the clean path may still bail.
func maybeLinkText(text string) bool {
	if strings.Contains(text, ".") {
		return true
	}
	// Caps-heavy hype arrives as HTTPS://…; ContainsFold keeps the pre-filter
	// aligned with stripScheme without allocating a lowered copy.
	return containsFoldASCII(text, "http://") || containsFoldASCII(text, "https://")
}

func nextSpaceToken(text string, i int) (start, end int) {
	for i < len(text) && isSpaceByte(text[i]) {
		i++
	}
	start = i
	for i < len(text) && !isSpaceByte(text[i]) {
		i++
	}
	return start, i
}

func isSpaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// trimLinkToken strips a leading http(s) scheme, a leading www., and trailing
// prose punctuation so "HTTPS://www.clips.twitch.tv/Slug!" compares as a clip.
func trimLinkToken(tok string) string {
	tok = stripScheme(tok)
	if len(tok) >= 4 && equalFoldASCII(tok[:4], "www.") {
		tok = tok[4:]
	}
	return stripTrailingPunct(tok)
}

func stripScheme(tok string) string {
	switch {
	case len(tok) >= 8 && equalFoldASCII(tok[:8], "https://"):
		return tok[8:]
	case len(tok) >= 7 && equalFoldASCII(tok[:7], "http://"):
		return tok[7:]
	}
	return tok
}

func stripTrailingPunct(tok string) string {
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

// looksLikeLinkHost reports whether tok (already trimmed) has a DNS-shaped
// host worth judging as a link. Rejects ellipsis debris and single-label
// tokens so the clips_only delete cannot fire on ordinary prose.
func looksLikeLinkHost(tok string) bool {
	host := hostOf(tok)
	if len(host) < 4 || len(host) > 253 {
		return false
	}
	labels := 0
	lastDot := true
	alphaTLD := false
	for i := 0; i < len(host); i++ {
		c := host[i]
		if c == '.' {
			if lastDot {
				return false
			}
			labels++
			lastDot = true
			alphaTLD = false
			continue
		}
		lastDot = false
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		switch {
		case c == '-' || c == '_':
		case c >= 'a' && c <= 'z':
			alphaTLD = true
		case c >= '0' && c <= '9':
			alphaTLD = false
		default:
			return false
		}
	}
	if lastDot {
		return false
	}
	labels++
	return labels >= 2 && alphaTLD
}

func hostOf(token string) string {
	host := token
	if k := strings.IndexAny(host, "/?#"); k >= 0 {
		host = host[:k]
	}
	if k := strings.LastIndexByte(host, '@'); k >= 0 {
		host = host[k+1:]
	}
	if k := strings.LastIndexByte(host, ':'); k >= 0 && isPortSuffix(host[k+1:]) {
		host = host[:k]
	}
	return host
}

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

// isTwitchClipToken reports whether a trimmed token is a Twitch clip URL.
// Matching is ASCII-fold only: clip slugs and logins are ASCII in practice,
// and folding the whole token avoids a lowered allocation on every candidate.
func isTwitchClipToken(tok string) bool {
	const clipsHost = "clips.twitch.tv/"
	const twitchHost = "twitch.tv/"
	const clipPath = "/clip/"
	if hasFoldPrefix(tok, clipsHost) {
		return clipSlugOK(tok[len(clipsHost):])
	}
	if !hasFoldPrefix(tok, twitchHost) {
		return false
	}
	rest := tok[len(twitchHost):]
	// login/clip/slug - login must be non-empty (Index of /clip/ > 0).
	i := indexFold(rest, clipPath)
	if i <= 0 {
		return false
	}
	return clipSlugOK(rest[i+len(clipPath):])
}

func clipSlugOK(rest string) bool {
	if k := strings.IndexAny(rest, "/?#"); k >= 0 {
		rest = rest[:k]
	}
	if len(rest) == 0 {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
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

func hasFoldPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && equalFoldASCII(s[:len(prefix)], prefix)
}

func indexFold(s, sub string) int {
	n := len(sub)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		if equalFoldASCII(s[i:i+n], sub) {
			return i
		}
	}
	return -1
}

func containsFoldASCII(s, sub string) bool {
	return indexFold(s, sub) >= 0
}
