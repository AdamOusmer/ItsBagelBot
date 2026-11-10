// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"strings"

	"ItsBagelBot/app/twitch/sesame/automod/linkcheck"
)

func hasNonClipLink(text string) bool {
	for i := 0; i < len(text); {
		start, end := linkcheck.NextToken(text, i)
		if start == end {
			return false
		}
		if linkToken(linkcheck.TrimLinkToken(text[start:end])).nonClipLink() {
			return true
		}
		i = end
	}
	return false
}

type linkToken string

func (tok linkToken) nonClipLink() bool {
	if tok == "" || isTwitchClipToken(string(tok)) {
		return false
	}
	return looksLikeLinkHost(string(tok))
}

func maybeLinkText(text string) bool {
	if strings.Contains(text, ".") {
		return true
	}
	return containsFoldASCII(text, "http://") || containsFoldASCII(text, "https://")
}

func looksLikeLinkHost(tok string) bool {
	host := linkcheck.HostOf(tok)
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

func hasFoldPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && linkcheck.EqualFoldASCII(s[:len(prefix)], prefix)
}

func indexFold(s, sub string) int {
	n := len(sub)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		if linkcheck.EqualFoldASCII(s[i:i+n], sub) {
			return i
		}
	}
	return -1
}

func containsFoldASCII(s, sub string) bool {
	return indexFold(s, sub) >= 0
}
