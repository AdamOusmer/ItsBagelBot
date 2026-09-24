// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

func iterLinkTokens(text string, fn func(token string)) {
	for i := 0; i < len(text); {
		start, end := nextToken(text, i)
		if start == end {
			return
		}
		if tok := trimLinkToken(text[start:end]); tok != "" && strings.Contains(tok, ".") {
			fn(tok)
		}
		i = end
	}
}

func nextToken(text string, i int) (start, end int) {
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

func validHost(h string) bool {
	return len(h) >= 4 && len(h) <= 253 && hostBytesOK(h) && hostLabelsOK(h)
}

func hostBytesOK(h string) bool {
	for i := 0; i < len(h); i++ {
		c := h[i]
		switch {
		case c == '.' || c == '-' || c == '_', 'a' <= c && c <= 'z', '0' <= c && c <= '9':
		default:
			return false
		}
	}
	return true
}

func hostLabelsOK(h string) bool {
	labels := 1
	last := byte('.')
	for i := 0; i < len(h); i++ {
		if h[i] == '.' {
			if last == '.' {
				return false
			}
			labels++
		}
		last = h[i]
	}
	return labels >= 2 && last != '.' && hostTLDOK(h)
}

func hostTLDOK(h string) bool {
	tld := h[strings.LastIndexByte(h, '.')+1:]
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

func foldHost(h string) string {
	folded, err := publicsuffix.EffectiveTLDPlusOne(h)
	if err != nil || folded == "" {
		return h
	}
	return folded
}
