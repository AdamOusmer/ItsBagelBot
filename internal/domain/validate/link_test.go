package validate

import "testing"

// links must all be caught. This is the anti-bypass corpus: plain URLs,
// scheme tricks, spelled-out and defanged separators, invisible splitters,
// unicode glyph folding, IPs, and structural URL tails. Invisible and
// full-width characters are written as \u escapes so the source stays clean.
func TestContainsLinkCatches(t *testing.T) {
	links := []string{
		// plain
		"http://example.com",
		"https://example.com/path?q=1",
		"visit https://itsbagelbot.com now",
		"example.com",
		"see you at foo.io",
		"my.sub.domain.co.uk/page",
		"www.example.com",
		"WWW.EXAMPLE.COM",
		"grab it at brand.shop today",
		"check brand.xyz",
		// scheme variety
		"ftp://files.example.org",
		"mailto:someone@example.com",
		"someone@example.com",
		"tel:+15551234567",
		"javascript:alert(1)",
		"java script:alert(1)",
		"data:text/html,<b>hi</b>",
		"//evil.example.com/x",
		"HtTpS://Example.Com",
		// defanged / spelled out
		"example[.]com",
		"example(dot)com",
		"example {dot} com",
		"example dot com",
		"go to example . com",
		"hxxp://evil.example.com",
		"hxxps://evil.example.com",
		"reach me user (at) gmail dot com",
		"user @ host . com",
		"e x a m p l e . c o m",
		// glyph / invisible (ASCII \u escapes, no raw invisibles in source)
		"ｅｘａｍｐｌｅ.ｃｏｍ", // full-width example.com
		"exa​mple.com", // zero-width space
		"exam‌ple.com", // zero-width non-joiner
		"example\ufeff.com", // BOM mid-string (backslash escape: Go rejects a raw BOM)
		"exa­mple.com", // soft hyphen
		// ip
		"192.168.0.1",
		"http://127.0.0.1:8080/x",
		"[2001:db8::1]",
		// structural tail rescues an uncommon TLD
		"grab.zip/now",
		"host.example:8443/login",
	}
	for _, in := range links {
		if !ContainsLink(in) {
			t.Errorf("ContainsLink(%q) = false, want true (link slipped through)", in)
		}
	}
}

// clean gift notes must pass. Bias is toward catching links, so these are the
// realistic notes we still want to deliver: normal punctuation, abbreviations,
// versions, prices, @mentions without a domain, and emoji.
func TestContainsLinkAllows(t *testing.T) {
	clean := []string{
		"",
		"Happy birthday! Enjoy premium \U0001F96F",
		"Thanks for everything. You earned this.",
		"Congrats on the new stream, you deserve it!",
		"e.g. this is a note, i.e. a gift",
		"Call me at 3 p.m. tomorrow",
		"Dr. Smith says hi",
		"You're 1.5x the streamer you were",
		"It costs $4.99 a month, worth it",
		"Ping @bagelfan when you're live",
		"Ph.D done, treat yourself",
		"Season 2 was great, on to season 3",
		"Meet me at 12:30 sharp",
	}
	for _, in := range clean {
		if ContainsLink(in) {
			t.Errorf("ContainsLink(%q) = true, want false (false positive)", in)
		}
	}
}
