// Isomorphic link detection, kept in lock-step with the Go source of truth at
// internal/domain/validate/link.go. The dashboard uses it twice on the gift
// path: live in the browser (instant feedback while typing) and again in the
// server action (before the basket RPC). The transactions service runs the Go
// version a third time, so a link never reaches the recipient's email even if a
// client is bypassed. Keep the two implementations in sync when either changes.
//
// The strength is the pre-processing, not one mega-regex (trivially bypassed):
// the input is folded to a canonical form three ways and every pattern runs
// against each. See the Go file for the full rationale.

// normalizeForLink folds to a lower-cased canonical form with invisibles gone.
// JS String.normalize gives NFKC for free (fold full-width / compatibility
// glyphs); \p{Cf} covers zero-width spaces, joiners, BOM and soft hyphen.
function normalizeForLink(s: string): string {
	return s
		.normalize('NFKC')
		.replace(/[\r\n\t]/g, ' ')
		.replace(/[\p{Cf}\p{Cc}]/gu, '')
		.toLowerCase();
}

// deobfuscateLinks rewrites the spelled-out / defanged forms back to real URL
// shape. Bare " at " / " point " are deliberately NOT rewritten (common English
// words); a spelled-out "user at host dot com" is still caught because " dot "
// alone leaves a real domain ("host.com") to detect.
function deobfuscateLinks(s: string): string {
	for (const [from, to] of WORD_REPLACERS) {
		s = s.split(from).join(to);
	}
	s = s.replace(/([a-z0-9])\s*\.\s*([a-z0-9])/g, '$1.$2');
	s = s.replace(/([a-z0-9])\s*@\s*([a-z0-9])/g, '$1@$2');
	return s;
}

// Longer keys first so they win (hxxps before hxxp).
const WORD_REPLACERS: [string, string][] = [
	['[.]', '.'], ['(.)', '.'], ['{.}', '.'], ['<.>', '.'],
	['[dot]', '.'], ['(dot)', '.'], ['{dot}', '.'], [' dot ', '.'], [' d0t ', '.'],
	['[point]', '.'], ['(point)', '.'],
	['[punkt]', '.'],
	['[at]', '@'], ['(at)', '@'], ['{at}', '@'], [' arobase ', '@'],
	['hxxps', 'https'], ['hxxp', 'http'],
	['httpx', 'http'], ['h**p', 'http']
];

// Multi-letter TLDs worth spotting as a bare domain. Two-letter TLDs are matched
// generically (reTLD2, every ccTLD); anything not here is still caught the moment
// it looks like a real URL (scheme / // / path / www / @).
const CURATED_TLDS = [
	'com', 'net', 'org', 'edu', 'gov', 'mil', 'int', 'info', 'biz', 'name',
	'pro', 'aero', 'coop', 'jobs', 'travel', 'asia', 'cat', 'tel',
	'xxx', 'post', 'arpa',
	'app', 'dev', 'page', 'web', 'site', 'online', 'store', 'shop', 'tech',
	'xyz', 'club', 'live', 'blog', 'cloud', 'space', 'world', 'life', 'link',
	'click', 'top', 'vip', 'win', 'icu', 'fun', 'run', 'today', 'news',
	'media', 'email', 'network', 'digital', 'design', 'studio', 'agency',
	'solutions', 'services', 'group', 'team', 'work', 'zone', 'wtf', 'lol',
	'ninja', 'guru', 'host', 'website', 'press', 'wiki', 'download', 'stream',
	'chat', 'social', 'fans', 'art', 'games', 'game', 'video', 'tube', 'photo',
	'pics', 'gallery', 'plus', 'now', 'one', 'ltd', 'inc', 'llc', 'corp',
	'company', 'center', 'city', 'land', 'house', 'homes', 'rent', 'sale',
	'deals', 'shopping', 'market', 'money', 'cash', 'fund', 'finance', 'bank',
	'trade', 'exchange', 'capital', 'gold', 'wang', 'xin', 'ink', 'pub',
	'xn--[a-z0-9]{2,}'
].join('|');

const reScheme = /\b(?:https?|ftps?|sftp|ssh|wss?|mailto|tel|sms|magnet|steam|discord|ircs?|xmpp):/i;
const reAuthy = /[a-z][a-z0-9+.\-]{0,30}:\/\//i;
const reProtoRl = /(?:^|[^a-z0-9+.\-/])\/\/[a-z0-9]/i;
const reDataURI = /\bdata:\s*(?:[a-z]+\/|;base64)/i;
const reScript = /\b(?:java|vb)script\s*:/i;
const reWWW = /\bwww\d{0,3}\.[a-z0-9-]/i;
const reEmail = /[a-z0-9._%+\-]+@[a-z0-9-]+(?:\.[a-z0-9-]+)*\.[a-z]{2,}/i;
const reTLD2 = /\b[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.[a-z]{2}\b/i;
const reTLDCur = new RegExp('\\b(?:[a-z0-9-]+\\.)+(?:' + CURATED_TLDS + ')\\b', 'i');
const reTLDPath = /\b[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9-]{1,63})*\.[a-z]{2,63}(?::\d{1,5})?[/?#]/i;
const reIPv4 = /\b(?:(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\b/;
const reIPv6 = /\[[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}\]/i;

// Full set: run against the normalized and deobfuscated forms.
const LINK_PATTERNS = [
	reScheme, reAuthy, reProtoRl, reDataURI, reScript,
	reWWW, reEmail, reTLD2, reTLDCur, reTLDPath, reIPv4, reIPv6
];

// High-signal set: run against the whitespace-stripped form. The generic
// two-letter-TLD rule is excluded there - it would turn "No. Ok" into a hit
// once spaces are gone.
const STRONG_PATTERNS = [
	reScheme, reAuthy, reProtoRl, reDataURI, reScript,
	reWWW, reEmail, reTLDCur, reIPv4
];

/**
 * containsLink reports whether s carries anything that could act as a link: a
 * URL scheme, a bare domain, an email/mailto target, an IP literal, or an
 * obfuscated form of any of those. Biased toward catching links (recall over
 * precision): a note that trips it is refused, not silently mangled.
 */
export function containsLink(s: string): boolean {
	if (!s) return false;
	const normalized = normalizeForLink(s);
	const deobfuscated = deobfuscateLinks(normalized);
	const despaced = deobfuscated.replace(/\s+/g, '');

	for (const re of LINK_PATTERNS) {
		if (re.test(normalized) || re.test(deobfuscated)) return true;
	}
	for (const re of STRONG_PATTERNS) {
		if (re.test(despaced)) return true;
	}
	return false;
}
