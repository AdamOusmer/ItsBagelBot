import { describe, expect, test } from 'bun:test';
import { containsLink } from './validation';

// Invisible / full-width characters are built from code points so the source
// stays ASCII-clean (a raw BOM is even illegal in some toolchains).
const ZWSP = String.fromCodePoint(0x200b);
const ZWNJ = String.fromCodePoint(0x200c);
const BOM = String.fromCodePoint(0xfeff);
const SHY = String.fromCodePoint(0x00ad);
const FULLWIDTH_EXAMPLE_COM = [0xff45, 0xff58, 0xff41, 0xff4d, 0xff50, 0xff4c, 0xff45]
	.map((c) => String.fromCodePoint(c))
	.join('') + '.' + [0xff43, 0xff4f, 0xff4d].map((c) => String.fromCodePoint(c)).join('');

describe('containsLink', () => {
	// Anti-bypass corpus: every one of these must be caught. Kept in sync with
	// internal/domain/validate/link_test.go.
	test('catches links and obfuscated links', () => {
		const links = [
			'http://example.com',
			'https://example.com/path?q=1',
			'visit https://itsbagelbot.com now',
			'example.com',
			'see you at foo.io',
			'my.sub.domain.co.uk/page',
			'www.example.com',
			'WWW.EXAMPLE.COM',
			'grab it at brand.shop today',
			'check brand.xyz',
			'ftp://files.example.org',
			'mailto:someone@example.com',
			'someone@example.com',
			'tel:+15551234567',
			'javascript:alert(1)',
			'java script:alert(1)',
			'data:text/html,<b>hi</b>',
			'//evil.example.com/x',
			'HtTpS://Example.Com',
			'example[.]com',
			'example(dot)com',
			'example {dot} com',
			'example dot com',
			'go to example . com',
			'hxxp://evil.example.com',
			'hxxps://evil.example.com',
			'reach me user (at) gmail dot com',
			'user @ host . com',
			'e x a m p l e . c o m',
			FULLWIDTH_EXAMPLE_COM,
			`exa${ZWSP}mple.com`,
			`exam${ZWNJ}ple.com`,
			`example${BOM}.com`,
			`exa${SHY}mple.com`,
			'192.168.0.1',
			'http://127.0.0.1:8080/x',
			'[2001:db8::1]',
			'grab.zip/now',
			'host.example:8443/login'
		];
		for (const link of links) {
			expect(containsLink(link)).toBe(true);
		}
	});

	// Realistic clean notes must pass (no false positives on normal punctuation).
	test('allows clean gift notes', () => {
		const clean = [
			'',
			'Happy birthday! Enjoy premium',
			'Thanks for everything. You earned this.',
			'Congrats on the new stream, you deserve it!',
			'e.g. this is a note, i.e. a gift',
			'Call me at 3 p.m. tomorrow',
			'Dr. Smith says hi',
			"You're 1.5x the streamer you were",
			'It costs $4.99 a month, worth it',
			"Ping @bagelfan when you're live",
			'Ph.D done, treat yourself',
			'Season 2 was great, on to season 3',
			'Meet me at 12:30 sharp'
		];
		for (const note of clean) {
			expect(containsLink(note)).toBe(false);
		}
	});
});
