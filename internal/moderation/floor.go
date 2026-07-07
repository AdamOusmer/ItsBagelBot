// Package moderation holds the content primitives shared by everything that
// judges or emits user-authored text: the skeleton normalizer (NFKC +
// confusable fold), the Aho-Corasick matcher, the categorized lexicon artifact,
// and the immovable floor. sesame's inline automod builds its gate on these,
// and the trust-boundary validators (internal/domain/validate) use CheckFloor
// so the commands/modules services reject floor content at save time - the bot
// must never store or emit it regardless of any per-channel setting.
package moderation

import "bytes"

// The infrastructure floor: objectively-abusive hosts and bait, safe to keep in
// source (no slur ever sits here; those live in the lexicon artifact). Matched
// as substrings against the normalized skeleton.
var (
	// IPLoggerDomains are IP-grabber/logger hosts used to dox, swat or DDoS.
	IPLoggerDomains = []string{
		"grabify.link", "iplogger.org", "iplogger.com", "iplogger.ru",
		"2no.co", "yip.su", "blasze.com", "stopify.co", "ps3cfw.com", "ipgrabber",
	}
	// ScamTerms are classic chat-scam bait phrases. They are chat-floor only:
	// CheckFloor (dashboard save-time) deliberately excludes them, because a
	// broadcaster's own giveaway command legitimately says "claim your prize".
	ScamTerms = []string{
		"free bits", "free gift sub", "free nitro", "cheap followers",
		"cheap viewers", "buy followers", "claim your prize",
	}
)

// Terms returns the raw terms loaded for a category (rule reporting, tests).
func (l *Lexicon) Terms(c Category) []string {
	if l == nil {
		return nil
	}
	return l.terms[c]
}

// CheckFloor reports whether text carries immovable-floor content: an identity
// slur (hate lexicon, word-bounded over the skeleton so leet and lookalike
// obfuscation folds onto it) or an IP-logger/grabber host. This is the save-time
// gate for dashboard-authored text (custom commands, module templates): the bot
// posts that text as itself, so hosting hate or dox infrastructure there risks
// the broadcaster's channel AND the bot account platform-wide. Everything
// milder (profanity, scam-sounding phrasing) is deliberately allowed - people
// say what they want, the floor is only hate and abuse infrastructure.
//
// Returns the offending term and true on a hit.
func CheckFloor(text string) (string, bool) {
	if text == "" {
		return "", false
	}
	skel := Normalize(nil, text)
	if len(skel) == 0 {
		return "", false
	}

	for _, d := range IPLoggerDomains {
		if bytes.Contains(skel, []byte(d)) {
			return d, true
		}
	}

	padded := make([]byte, 0, len(skel)+2)
	padded = append(padded, ' ')
	padded = append(padded, skel...)
	padded = append(padded, ' ')
	if cat, term := EmbeddedLexicon().Scan(padded, true); cat == CatHate {
		return term, true
	}
	return "", false
}
