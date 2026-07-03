package validate

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ContainsLink reports whether s carries anything that could act as a link: a
// URL scheme, a bare domain, an email/mailto target, an IP literal, or an
// obfuscated form of any of those. It is deliberately biased toward catching
// links (recall over precision): a gift note that trips it is rejected and the
// buyer is asked to rephrase, which is the right trade for a note we email to
// another user. A handful of link-shaped phrases ("Dr. No", "see you.io") are
// caught as false positives on purpose; nothing link-like is let through.
//
// The strength is in the pre-processing, not a single mega-regex. A regex alone
// is trivially bypassed (example[.]com, exa<zero-width>mple.com, ｅｘａｍｐｌｅ.com,
// "example dot com", "example . com"). So the input is first folded to a
// canonical form three different ways, and every pattern runs against each:
//
//  1. normalized  - NFKC (fold full-width / compatibility glyphs), strip
//     invisibles (zero-width, BOM, bidi overrides, soft hyphen) and control
//     runes, lower-case. Kills glyph and invisible-splitting tricks.
//  2. deobfuscated - the above, plus textual dot/at spellings ("(dot)", "[.]",
//     " dot ", "hxxp", "(at)") rewritten and spaces around dots/@ collapsed.
//     Kills spacing and spelled-out tricks.
//  3. despaced     - the deobfuscated form with all whitespace removed, scanned
//     with the high-signal patterns only (scheme, www, known TLD, IP). Catches
//     "e x a m p l e . c o m" without turning every sentence break into a hit.
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

// normalizeForLink folds the input to a canonical, lower-cased form with every
// invisible and formatting rune removed so a scanner sees the real characters.
func normalizeForLink(s string) string {

	s = norm.NFKC.String(s)

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			b.WriteByte(' ')
		case r == 0x200B || r == 0x200C || r == 0x200D || r == 0x2060 || r == 0xFEFF || r == 0x00AD:
			// Explicit zero-width / joiners / BOM / soft hyphen: the classic
			// "exa​mple.com" splitter. Also covered by unicode.Cf below,
			// but named here for intent.
			continue
		case unicode.Is(unicode.Cf, r) || unicode.IsControl(r):
			// Format category (bidi overrides, variation selectors) and any
			// other control rune: dropped, they only serve to hide structure.
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

// deobfuscateLinks rewrites the common ways a link is spelled to slip past a
// naive scanner back into canonical URL/domain shape.
func deobfuscateLinks(s string) string {

	s = linkWordReplacer.Replace(s)
	// Collapse spacing around dots and @ so "example . com" / "user @ host"
	// become "example.com" / "user@host". Only fires between alphanumerics, so
	// ordinary "end. Start" is joined (to "end.start") but only *detected* if
	// the result carries a real TLD - "start" is not one, so prose survives.
	s = spacedDot.ReplaceAllString(s, "$1.$2")
	s = spacedAt.ReplaceAllString(s, "$1@$2")
	return s
}

// linkWordReplacer maps spelled-out and defanged separators to real ones. Only
// unambiguous forms are rewritten: bracketed/parenthesized spellings, and the
// bare " dot " (whose sole false positive would be prose literally about a
// domain). Bare " at " / " point " are deliberately NOT rewritten - they are
// common English words, and a spelled-out "user at host dot com" is still
// caught because " dot " alone leaves a real domain ("host.com") to detect.
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

// curatedTLDs are the multi-letter TLDs worth spotting as a bare domain (no
// scheme, no path). Two-letter TLDs are matched generically (reTLD2, covers
// every ccTLD); this list adds the common gTLDs plus the punycode prefix so a
// scheme-less "brand.shop" or "brand.xn--..." is caught. Anything not here is
// still caught the moment it looks like a real URL (scheme / // / path / www /
// @), via the structural patterns below.
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
	// URL scheme, either as a known link scheme or any "scheme://" authority.
	reScheme  = regexp.MustCompile(`(?i)\b(?:https?|ftps?|sftp|ssh|wss?|mailto|tel|sms|magnet|steam|discord|ircs?|xmpp):`)
	reAuthy   = regexp.MustCompile(`(?i)[a-z][a-z0-9+.\-]{0,30}://`)
	reProtoRl = regexp.MustCompile(`(?:^|[^a-z0-9+.\-/])//[a-z0-9]`)
	reDataURI = regexp.MustCompile(`(?i)\bdata:\s*(?:[a-z]+/|;base64)`)
	reScript  = regexp.MustCompile(`(?i)\b(?:java|vb)script\s*:`)

	// www.host - a domain even without a scheme.
	reWWW = regexp.MustCompile(`(?i)\bwww\d{0,3}\.[a-z0-9\-]`)

	// user@host.tld - becomes a mailto: link in most clients.
	reEmail = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9\-]+(?:\.[a-z0-9\-]+)*\.[a-z]{2,}`)

	// host.<2-letter> - covers every ccTLD generically.
	reTLD2 = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?\.[a-z]{2}\b`)

	// host.<curated gTLD> - a bare domain with a common multi-letter TLD.
	reTLDCur = regexp.MustCompile(`(?i)\b(?:[a-z0-9\-]+\.)+(?:` + curatedTLDs + `)\b`)

	// host.<any letters> immediately followed by a port/path/query/fragment:
	// the structural tail is what proves it is a URL, so any TLD is fair game.
	reTLDPath = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?(?:\.[a-z0-9\-]{1,63})*\.[a-z]{2,63}(?::\d{1,5})?[/?#]`)

	// IPv4 dotted quad and bracketed IPv6.
	reIPv4 = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\b`)
	reIPv6 = regexp.MustCompile(`\[[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}\]`)
)

// linkPatterns run against the normalized and deobfuscated forms.
var linkPatterns = []*regexp.Regexp{
	reScheme, reAuthy, reProtoRl, reDataURI, reScript,
	reWWW, reEmail, reTLD2, reTLDCur, reTLDPath, reIPv4, reIPv6,
}

// strongLinkPatterns run against the whitespace-stripped form. Only the
// high-signal patterns are used there: the generic two-letter-TLD rule would
// turn ordinary "No. Ok" into a hit once spaces are gone, so it is excluded.
var strongLinkPatterns = []*regexp.Regexp{
	reScheme, reAuthy, reProtoRl, reDataURI, reScript,
	reWWW, reEmail, reTLDCur, reIPv4,
}
