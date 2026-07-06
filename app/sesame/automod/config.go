package automod

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Profile is a broadcaster's content-tolerance tier. It tunes the heuristic
// sensitivity; it NEVER touches the immovable floor. Objectively-abusive
// infrastructure (IP-logger/grabber domains, scam bait) and obfuscation
// (zero-width injection, rune floods) are enforced under every profile, so a
// broadcaster cannot profile their channel into hosting a hate raid's payload.
type Profile uint8

const (
	// ProfileModerate is the balanced default (teen-ish): the standard caps and
	// symbol heuristics apply.
	ProfileModerate Profile = iota
	// ProfilePG is family/strict: tighter caps sensitivity.
	ProfilePG
	// ProfileAdult is 18+: the caps and symbol nags are off entirely; only the
	// floor and obfuscation signals remain.
	ProfileAdult
)

func parseProfile(s string) Profile {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pg", "family", "strict":
		return ProfilePG
	case "adult", "18+", "mature":
		return ProfileAdult
	default:
		return ProfileModerate
	}
}

func (p Profile) String() string {
	switch p {
	case ProfilePG:
		return "pg"
	case ProfileAdult:
		return "adult"
	default:
		return "moderate"
	}
}

// Config is one broadcaster's automod settings, parsed from the "automod"
// ModuleView.Configs blob. A nil *Config (no row for the channel) behaves exactly
// like the global default: moderate profile, enabled, no custom terms. block/allow
// terms are normalized into the same skeleton space the blocklist scans, so they
// match through the same obfuscation folding.
type Config struct {
	Disabled   bool
	Profile    Profile
	blockTerms [][]byte // channel-added blocked substrings (skeleton space)
	allowTerms [][]byte // channel-permitted substrings; suppress non-floor flags
}

// wireConfig is the JSON shape stored in the automod ModuleView.Configs blob.
// The dashboard module form writes flat string values (one field per key, see
// MODULE_CATALOG id "automod" in console/shared/lib/types.ts), so the term lists
// are a single comma- or newline-separated string, not JSON arrays. The enable
// toggle is NOT here: it is the module row's own is_enabled flag.
type wireConfig struct {
	Profile    string `json:"profile"`
	BlockTerms string `json:"block_terms"`
	AllowTerms string `json:"allow_terms"`
}

// ParseConfig decodes a ModuleView.Configs blob. An empty or malformed blob
// yields nil, which the gate treats as the global default (fail-open to the
// built-in behavior, never fail-closed to "block everything").
func ParseConfig(raw json.RawMessage) *Config {
	if len(raw) == 0 {
		return nil
	}
	var w wireConfig
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil
	}
	return &Config{
		Profile:    parseProfile(w.Profile),
		blockTerms: normalizeTerms(splitTerms(w.BlockTerms)),
		allowTerms: normalizeTerms(splitTerms(w.AllowTerms)),
	}
}

// splitTerms breaks a dashboard textarea value into individual terms: commas and
// newlines both separate, surrounding whitespace is trimmed, empties dropped.
func splitTerms(s string) []string {
	if s == "" {
		return nil
	}
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	out := fields[:0]
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// normalizeTerms folds each term into the skeleton space (NFKC + confusable fold
// + strip invisibles + lowercase) so channel terms match the same way the
// blocklist does. Empty results are dropped.
func normalizeTerms(terms []string) [][]byte {
	if len(terms) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(terms))
	for _, t := range terms {
		n := Normalize(nil, t)
		if len(n) > 0 {
			out = append(out, n)
		}
	}
	return out
}

// disabled reports whether the config opts the whole gate out for this channel.
func (c *Config) disabled() bool { return c != nil && c.Disabled }

// profile returns the effective profile (moderate for a nil config).
func (c *Config) profile() Profile {
	if c == nil {
		return ProfileModerate
	}
	return c.Profile
}

// allows reports whether the skeleton contains a channel allow-term, which
// suppresses a non-floor flag (heuristic or channel block-term). Floor matches
// never consult this.
func (c *Config) allows(skel []byte) bool {
	if c == nil {
		return false
	}
	return containsAny(skel, c.allowTerms)
}

// containsAny reports whether skel contains any of terms.
func containsAny(skel []byte, terms [][]byte) bool {
	for _, t := range terms {
		if bytes.Contains(skel, t) {
			return true
		}
	}
	return false
}

// profileHeuristics maps a profile onto the caps threshold and whether the caps
// and symbol heuristics apply at all. Obfuscation signals (zero-width, rune
// floods) are profile-independent and handled by the caller.
func profileHeuristics(p Profile) (capsThresh float64, capsOn, symbolOn bool) {
	switch p {
	case ProfilePG:
		return 0.6, true, true
	case ProfileAdult:
		return 0.9, false, false
	default:
		return capsThreshold, true, true
	}
}
