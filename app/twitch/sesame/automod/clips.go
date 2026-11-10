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
		if isNonClipLink(linkcheck.TrimLinkToken(text[start:end])) {
			return true
		}
		i = end
	}
	return false
}

func isNonClipLink(tok string) bool {
	if tok == "" || isTwitchClipToken(tok) {
		return false
	}
	return looksLikeLinkHost(tok)
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
	labels, ok := hostLabelCount(host)
	return ok && labels >= 2 && endsInLetter(host)
}

func hostLabelCount(host string) (int, bool) {
	labels := 1
	lastDot := true
	for i := 0; i < len(host); i++ {
		if !isLinkHostByte(host[i]) {
			return 0, false
		}
		isDot := host[i] == '.'
		if isDot && lastDot {
			return 0, false
		}
		if isDot {
			labels++
		}
		lastDot = isDot
	}
	return labels, !lastDot
}

func isLinkHostByte(c byte) bool {
	return c == '.' || c == '-' || c == '_' || isAlnumByte(c)
}

func isAlnumByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func endsInLetter(host string) bool {
	for i := len(host) - 1; i >= 0; i-- {
		c := host[i]
		switch {
		case c == '.':
			return false
		case c >= '0' && c <= '9':
			return false
		case c != '-' && c != '_':
			return true
		}
	}
	return false
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
		if !isSlugByte(rest[i]) {
			return false
		}
	}
	return true
}

func isSlugByte(c byte) bool {
	return isAlnumByte(c) || c == '-' || c == '_'
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
