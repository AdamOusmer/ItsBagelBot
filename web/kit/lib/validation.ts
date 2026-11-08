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

const reScheme = /\b(?:https?|ftps?|sftp|ssh|wss?|mailto|tel|sms|magnet|steam|ircs?|xmpp):/i;
const reAuthy = /[a-z][a-z0-9+.\-]{0,30}:\/\//i;
const reProtoRl = /(?:^|[^a-z0-9+.\-/])\/\/[a-z0-9]/i;
const reDataURI = /\bdata:\s*(?:[a-z]+\/|;base64)/i;
const reScript = /\b(?:java|vb)script\s*:/i;
const reWWW = /\bwww\d{0,3}\.[a-z0-9-]/i;
const reTLD2 = /\b[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.[a-z]{2}\b/i;
const reTLDCur = new RegExp('\\b(?:[a-z0-9-]+\\.)+(?:' + CURATED_TLDS + ')\\b', 'i');
const reTLDPath = /\b[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9-]{1,63})*\.[a-z]{2,63}(?::\d{1,5})?[/?#]/i;
const reIPv4 = /\b(?:(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\b/;
const reIPv6 = /\[[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}\]/i;

const LINK_PATTERNS = [
	reScheme, reAuthy, reProtoRl, reDataURI, reScript,
	reWWW, reTLD2, reTLDCur, reTLDPath, reIPv4, reIPv6
];

const STRONG_PATTERNS = [
	reScheme, reAuthy, reProtoRl, reDataURI, reScript,
	reWWW, reTLDCur, reIPv4
];

function isLetter(c: number): boolean {
	return c >= 97 && c <= 122;
}

function isDigit(c: number): boolean {
	return c >= 48 && c <= 57;
}

function isLocalChar(s: string, i: number): boolean {
	const c = s.charCodeAt(i);
	return isLetter(c) || isDigit(c) || '._%+-'.includes(s[i]);
}

function isDomainChar(c: number): boolean {
	return isLetter(c) || isDigit(c) || c === 45;
}

function endOfDomainLabel(s: string, start: number): number {
	let i = start;
	while (i < s.length && isDomainChar(s.charCodeAt(i))) i++;
	return i;
}

function hasEmailDomain(s: string, start: number): boolean {
	let end = endOfDomainLabel(s, start);
	if (end === start) return false;
	while (s.charCodeAt(end) === 46) {
		if (isLetter(s.charCodeAt(end + 1)) && isLetter(s.charCodeAt(end + 2))) return true;
		const next = end + 1;
		end = endOfDomainLabel(s, next);
		if (end === next) return false;
	}
	return false;
}

// Domain scans after different @ signs do not overlap; the whole scan is linear.
function containsEmail(s: string): boolean {
	let local = false;
	for (let i = 0; i < s.length; i++) {
		if (s.charCodeAt(i) === 64 && local && hasEmailDomain(s, i + 1)) return true;
		local = isLocalChar(s, i);
	}
	return false;
}

interface LinkText {
	normalized: string;
	deobfuscated: string;
	despaced: string;
}

function linkText(s: string): LinkText {
	const normalized = normalizeForLink(s);
	const deobfuscated = deobfuscateLinks(normalized);
	return { normalized, deobfuscated, despaced: deobfuscated.replace(/\s+/g, '') };
}

function matchesAny(text: string, patterns: readonly RegExp[]): boolean {
	return patterns.some((pattern) => pattern.test(text));
}

function hasEmail(text: LinkText): boolean {
	return [text.normalized, text.deobfuscated, text.despaced].some(containsEmail);
}

function hasLinkPattern(text: LinkText): boolean {
	return matchesAny(text.normalized, LINK_PATTERNS)
		|| matchesAny(text.deobfuscated, LINK_PATTERNS)
		|| matchesAny(text.despaced, STRONG_PATTERNS);
}

export function containsLink(s: string): boolean {
	if (!s) return false;
	const text = linkText(s);
	return hasEmail(text) || hasLinkPattern(text);
}

export function normalizeCounterName(raw: unknown): string {
	return String(raw ?? '')
		.trim()
		.replace(/^!/, '')
		.toLowerCase()
		.slice(0, 64);
}

export { MAX_COUNTER_VALUE, parseCounterValue, isCounterValue, formatCounterValue } from './counter-value';

export function clampInt(raw: unknown, min: number, max: number, dflt: number): number {
	const n = Math.trunc(Number(raw));
	if (!Number.isFinite(n)) return dflt;
	return Math.min(max, Math.max(min, n));
}

export { parsePointValue, readPointBalance, formatPointValue } from './point-value';
