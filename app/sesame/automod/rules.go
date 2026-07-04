package automod

// The curated starter blocklist. Real slur/hate lists load from the hot-reload
// pattern artifact in a later phase and are intentionally NOT hardcoded here; the
// concrete starter set is objectively-abusive infrastructure (IP-logger/grabber
// domains) and common scam bait, which are safe to keep in source. Terms are
// matched as substrings against the normalized skeleton (lowercase latin), so
// they must be written that way.
var (
	ipLoggerDomains = []string{
		"grabify.link", "iplogger.org", "iplogger.com", "iplogger.ru",
		"2no.co", "yip.su", "blasze.com", "stopify.co", "ps3cfw.com", "ipgrabber",
	}
	scamTerms = []string{
		"free bits", "free gift sub", "free nitro", "cheap followers",
		"cheap viewers", "buy followers", "claim your prize",
	}
)

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
		{name: "ip_logger", terms: toBytes(ipLoggerDomains), action: ActionTimeout, seconds: 600},
		{name: "scam", terms: toBytes(scamTerms), action: ActionTimeout, seconds: 600},
	}
}
