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

func StripLinks(text string) string {
	var b strings.Builder
	kept := 0
	for i := 0; i < len(text); {
		start, end := linkcheck.NextToken(text, i)
		if start == end {
			break
		}
		if looksLikeLinkHost(linkToken(linkcheck.TrimLinkToken(text[start:end]))) {
			b.WriteString(text[kept:start])
			kept = end
		}
		i = end
	}
	if kept == 0 {
		return text
	}
	b.WriteString(text[kept:])
	return strings.Join(strings.Fields(b.String()), " ")
}

type linkToken string

type foldText string

func (tok linkToken) nonClipLink() bool {
	if tok == "" || isTwitchClipToken(tok) {
		return false
	}
	return looksLikeLinkHost(tok)
}

func maybeLinkText(text string) bool {
	if strings.Contains(text, ".") {
		return true
	}
	return containsFoldASCII(foldText(text), "http://") || containsFoldASCII(foldText(text), "https://")
}

func looksLikeLinkHost(tok linkToken) bool {
	host := linkcheck.HostOf(string(tok))
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

func isTwitchClipToken(tok linkToken) bool {
	const clipsHost = "clips.twitch.tv/"
	const twitchHost = "twitch.tv/"
	const clipPath = "/clip/"
	text := foldText(tok)
	if hasFoldPrefix(text, clipsHost) {
		return clipSlugOK(text[len(clipsHost):])
	}
	if !hasFoldPrefix(text, twitchHost) {
		return false
	}
	rest := text[len(twitchHost):]
	i := indexFold(rest, clipPath)
	if i <= 0 {
		return false
	}
	return clipSlugOK(rest[i+len(clipPath):])
}

func clipSlugOK(rest foldText) bool {
	if k := strings.IndexAny(string(rest), "/?#"); k >= 0 {
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

func hasFoldPrefix(s, prefix foldText) bool {
	return len(s) >= len(prefix) && linkcheck.EqualFoldASCII(string(s[:len(prefix)]), string(prefix))
}

func indexFold(s, sub foldText) int {
	n := len(sub)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		if linkcheck.EqualFoldASCII(string(s[i:i+n]), string(sub)) {
			return i
		}
	}
	return -1
}

func containsFoldASCII(s, sub foldText) bool {
	return indexFold(s, sub) >= 0
}
