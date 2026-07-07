package automod

import "ItsBagelBot/internal/moderation"

// The infrastructure floor lists live in internal/moderation (shared with the
// save-time validators); this file only maps them onto chat verdicts. Terms are
// matched as substrings against the normalized skeleton (lowercase latin).

// category is one named blocklist plus the action a match implies.
type category struct {
	name    string
	terms   [][]byte
	action  Action
	seconds uint32
}

func toBytes(ss []string) [][]byte {
	out := make([][]byte, len(ss))
	for i, s := range ss {
		out[i] = []byte(s)
	}
	return out
}

func defaultCategories() []category {
	return []category{
		{name: "ip_logger", terms: toBytes(moderation.IPLoggerDomains), action: ActionTimeout, seconds: 600},
		{name: "scam", terms: toBytes(moderation.ScamTerms), action: ActionTimeout, seconds: 600},
	}
}
