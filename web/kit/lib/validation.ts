// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

function normalizeForLink(s: string): string {
	return s
		.normalize('NFKC')
		.replace(/[\r\n\t]/g, ' ')
		.replace(/[\p{Cf}\p{Cc}]/gu, '')
		.toLowerCase();
}

function deobfuscateLinks(s: string): string {
	for (const [from, to] of WORD_REPLACERS) {
		s = s.split(from).join(to);
	}
	s = s.replace(/([a-z0-9])\s*\.\s*([a-z0-9])/g, '$1.$2');
	s = s.replace(/([a-z0-9])\s*@\s*([a-z0-9])/g, '$1@$2');
	return s;
}

const WORD_REPLACERS: [string, string][] = [
	['[.]', '.'], ['(.)', '.'], ['{.}', '.'], ['<.>', '.'],
	['[dot]', '.'], ['(dot)', '.'], ['{dot}', '.'], [' dot ', '.'], [' d0t ', '.'],
	['[point]', '.'], ['(point)', '.'],
	['[punkt]', '.'],
	['[at]', '@'], ['(at)', '@'], ['{at}', '@'], [' arobase ', '@'],
	['hxxps', 'https'], ['hxxp', 'http'],
	['httpx', 'http'], ['h**p', 'http']
];

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

const LINK_PATTERNS = [
	reScheme, reAuthy, reProtoRl, reDataURI, reScript,
	reWWW, reEmail, reTLD2, reTLDCur, reTLDPath, reIPv4, reIPv6
];

const STRONG_PATTERNS = [
	reScheme, reAuthy, reProtoRl, reDataURI, reScript,
	reWWW, reEmail, reTLDCur, reIPv4
];

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

export function normalizeCounterName(raw: unknown): string {
	return String(raw ?? '')
		.trim()
		.replace(/^!/, '')
		.toLowerCase()
		.slice(0, 64);
}

export const MAX_COUNTER_VALUE = 9223372036854775807n;

// JSON numbers above 2^53 lose digits. Counter RPC replies carry decimal
// strings; safe legacy numbers are accepted while services roll forward.
export function parseCounterValue(raw: unknown): string | null {
	if (typeof raw === 'number') {
		return Number.isSafeInteger(raw) && raw >= 0 ? String(raw) : null;
	}
	if (typeof raw !== 'string') return null;
	const digits = raw.trim();
	if (digits.length === 0 || digits.length > 19 || !/^\d+$/.test(digits)) return null;
	const value = BigInt(digits);
	return value <= MAX_COUNTER_VALUE ? value.toString() : null;
}

export function isCounterValue(raw: unknown): raw is string {
	return typeof raw === 'string' && parseCounterValue(raw) !== null;
}

export function formatCounterValue(raw: string, locale?: string): string {
	return BigInt(raw).toLocaleString(locale);
}

export function clampInt(raw: unknown, min: number, max: number, dflt: number): number {
	const n = Math.trunc(Number(raw));
	if (!Number.isFinite(n)) return dflt;
	return Math.min(max, Math.max(min, n));
}
