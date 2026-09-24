// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func ContainsLink(s string) bool {

	normalized := normalizeForLink(s)
	deobfuscated := deobfuscateLinks(normalized)
	despaced := whitespace.ReplaceAllString(deobfuscated, "")

	for _, re := range linkPatterns {
		if re.MatchString(normalized) || re.MatchString(deobfuscated) {
			return true
		}
	}
	for _, re := range strongLinkPatterns {
		if re.MatchString(despaced) {
			return true
		}
	}
	return false
}

func normalizeForLink(s string) string {

	s = norm.NFKC.String(s)

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			b.WriteByte(' ')
		case r == 0x200B || r == 0x200C || r == 0x200D || r == 0x2060 || r == 0xFEFF || r == 0x00AD:
			continue
		case unicode.Is(unicode.Cf, r) || unicode.IsControl(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

func deobfuscateLinks(s string) string {

	s = linkWordReplacer.Replace(s)
	s = spacedDot.ReplaceAllString(s, "$1.$2")
	s = spacedAt.ReplaceAllString(s, "$1@$2")
	return s
}

var linkWordReplacer = strings.NewReplacer(
	"[.]", ".", "(.)", ".", "{.}", ".", "<.>", ".",
	"[dot]", ".", "(dot)", ".", "{dot}", ".", " dot ", ".", " d0t ", ".",
	"[point]", ".", "(point)", ".",
	"[punkt]", ".",
	"[at]", "@", "(at)", "@", "{at}", "@", " arobase ", "@",
	"hxxps", "https", "hxxp", "http",
	"httpx", "http", "h**p", "http",
)

var (
	whitespace = regexp.MustCompile(`\s+`)
	spacedDot  = regexp.MustCompile(`([a-z0-9])\s*\.\s*([a-z0-9])`)
	spacedAt   = regexp.MustCompile(`([a-z0-9])\s*@\s*([a-z0-9])`)
)

var curatedTLDs = strings.Join([]string{
	"com", "net", "org", "edu", "gov", "mil", "int", "info", "biz", "name",
	"pro", "aero", "coop", "jobs", "mobi", "travel", "asia", "cat", "tel",
	"xxx", "post", "arpa",
	"app", "dev", "page", "web", "site", "online", "store", "shop", "tech",
	"xyz", "club", "live", "blog", "cloud", "space", "world", "life", "link",
	"click", "top", "vip", "win", "icu", "fun", "run", "today", "news",
	"media", "email", "network", "digital", "design", "studio", "agency",
	"solutions", "services", "group", "team", "work", "zone", "wtf", "lol",
	"ninja", "guru", "host", "website", "press", "wiki", "download", "stream",
	"chat", "social", "fans", "art", "games", "game", "video", "tube", "photo",
	"pics", "gallery", "plus", "now", "one", "ltd", "inc", "llc", "corp",
	"company", "center", "city", "land", "house", "homes", "rent", "sale",
	"deals", "shopping", "market", "money", "cash", "fund", "finance", "bank",
	"trade", "exchange", "capital", "gold", "wang", "xin", "ink", "pub",
	"xn--[a-z0-9]{2,}",
}, "|")

var (
	reScheme  = regexp.MustCompile(`(?i)\b(?:https?|ftps?|sftp|ssh|wss?|mailto|tel|sms|magnet|steam|discord|ircs?|xmpp):`)
	reAuthy   = regexp.MustCompile(`(?i)[a-z][a-z0-9+.\-]{0,30}://`)
	reProtoRl = regexp.MustCompile(`(?:^|[^a-z0-9+.\-/])//[a-z0-9]`)
	reDataURI = regexp.MustCompile(`(?i)\bdata:\s*(?:[a-z]+/|;base64)`)
	reScript  = regexp.MustCompile(`(?i)\b(?:java|vb)script\s*:`)

	reWWW = regexp.MustCompile(`(?i)\bwww\d{0,3}\.[a-z0-9\-]`)

	reEmail = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9\-]+(?:\.[a-z0-9\-]+)*\.[a-z]{2,}`)

	reTLD2 = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?\.[a-z]{2}\b`)

	reTLDCur = regexp.MustCompile(`(?i)\b(?:[a-z0-9\-]+\.)+(?:` + curatedTLDs + `)\b`)

	reTLDPath = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?(?:\.[a-z0-9\-]{1,63})*\.[a-z]{2,63}(?::\d{1,5})?[/?#]`)

	reIPv4 = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\b`)
	reIPv6 = regexp.MustCompile(`\[[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}\]`)
)

var linkPatterns = []*regexp.Regexp{
	reScheme, reAuthy, reProtoRl, reDataURI, reScript,
	reWWW, reEmail, reTLD2, reTLDCur, reTLDPath, reIPv4, reIPv6,
}

var strongLinkPatterns = []*regexp.Regexp{
	reScheme, reAuthy, reProtoRl, reDataURI, reScript,
	reWWW, reEmail, reTLDCur, reIPv4,
}
