package automod

import (
	"bytes"
	"encoding/json"
	"strings"

	"ItsBagelBot/internal/moderation"
)

// Level is a broadcaster's enforcement preset, spanning none -> all. It sets
// the default for every toggleable section; the per-section switches below
// override it. NO level touches the immovable floor: identity slurs and
// IP-grabber infrastructure are enforced under every setting, including
// "none" and a disabled module row, because hosting them risks the streamer's
// channel and the bot account platform-wide (Twitch ToS). Everything else is
// the broadcaster's call - people say what they want.
type Level uint8

const (
	// LevelModerate is the balanced default (zero value): harassment, sexual
	// content, style checks and campaign counting on; plain profanity allowed.
	LevelModerate Level = iota
	// LevelNone is floor-only: nothing but the immovable floor.
	LevelNone
	// LevelBasic adds directed harassment on top of the floor.
	LevelBasic
	// LevelStrict ("all") turns every section on, including plain profanity,
	// with a tighter caps threshold. Family/PG channels.
	LevelStrict
)

func parseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "off", "floor":
		return LevelNone
	case "basic", "adult", "18+", "mature":
		return LevelBasic
	case "strict", "all", "pg", "family":
		return LevelStrict
	default:
		return LevelModerate
	}
}

func (l Level) String() string {
	switch l {
	case LevelNone:
		return "none"
	case LevelBasic:
		return "basic"
	case LevelStrict:
		return "strict"
	default:
		return "moderate"
	}
}

// sections are the resolved per-category switches the gate runs under.
type sections struct {
	harassment bool // directed-harm phrases (warn + delete, ladder up)
	sexual     bool // explicit sexual terms (delete)
	profanity  bool // plain profanity (delete)
	style      bool // caps / symbol / repeat heuristics (delete)
	links      bool // count link templates across senders (campaign juror)
	capsThresh float64
}

// levelSections maps a preset onto its section defaults.
func levelSections(l Level) sections {
	switch l {
	case LevelNone:
		return sections{capsThresh: capsThreshold}
	case LevelBasic:
		return sections{harassment: true, capsThresh: capsThreshold}
	case LevelStrict:
		return sections{harassment: true, sexual: true, profanity: true, style: true, links: true, capsThresh: 0.6}
	default: // LevelModerate
		return sections{harassment: true, sexual: true, style: true, links: true, capsThresh: capsThreshold}
	}
}

// triState is a section override: unset follows the level, on/off forces it.
type triState uint8

const (
	triUnset triState = iota
	triOn
	triOff
)

func parseTri(s string) triState {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "1", "yes":
		return triOn
	case "off", "false", "0", "no":
		return triOff
	default:
		return triUnset
	}
}

func (t triState) apply(def bool) bool {
	switch t {
	case triOn:
		return true
	case triOff:
		return false
	default:
		return def
	}
}

// Config is one broadcaster's automod settings, parsed from the "automod"
// ModuleView.Configs blob. A nil *Config (no row for the channel) behaves
// exactly like the global default (LevelModerate). block/allow terms are
// normalized into the same skeleton space the blocklist scans, so they match
// through the same obfuscation folding.
type Config struct {
	// Disabled mirrors the module row's enable toggle being off. It is NOT a
	// full opt-out: it degrades the gate to floor-only (same as LevelNone), so
	// no channel setting can host what would endanger the account.
	Disabled bool
	Level    Level

	harassment, sexual, profanity, style, links triState

	blockTerms [][]byte // channel-added blocked substrings (skeleton space)
	allowTerms [][]byte // channel-permitted substrings; suppress non-floor flags
}

// wireConfig is the JSON shape stored in the automod ModuleView.Configs blob.
// The dashboard module form writes flat string values (one field per key, see
// MODULE_CATALOG id "automod" in console/shared/lib/types.ts): level is the
// preset select, the section keys are "on"/"off" toggles (empty = follow the
// level), and the term lists are comma- or newline-separated strings. The
// legacy "profile" key from the first config shape is honored as a level
// alias. The enable toggle is NOT here: it is the module row's own is_enabled.
type wireConfig struct {
	Level      string `json:"level"`
	Profile    string `json:"profile"` // legacy alias for level
	Harassment string `json:"harassment"`
	Sexual     string `json:"sexual"`
	Profanity  string `json:"profanity"`
	Style      string `json:"style"`
	Links      string `json:"links"`
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
	lvl := w.Level
	if lvl == "" {
		lvl = w.Profile
	}
	return &Config{
		Level:      parseLevel(lvl),
		harassment: parseTri(w.Harassment),
		sexual:     parseTri(w.Sexual),
		profanity:  parseTri(w.Profanity),
		style:      parseTri(w.Style),
		links:      parseTri(w.Links),
		blockTerms: normalizeTerms(splitTerms(w.BlockTerms)),
		allowTerms: normalizeTerms(splitTerms(w.AllowTerms)),
	}
}

// resolved returns the effective sections: the level's defaults with the
// explicit per-section overrides applied. A nil config is the global default;
// a disabled module row is floor-only regardless of anything else.
func (c *Config) resolved() sections {
	if c == nil {
		return levelSections(LevelModerate)
	}
	if c.Disabled {
		return levelSections(LevelNone)
	}
	s := levelSections(c.Level)
	s.harassment = c.harassment.apply(s.harassment)
	s.sexual = c.sexual.apply(s.sexual)
	s.profanity = c.profanity.apply(s.profanity)
	s.style = c.style.apply(s.style)
	s.links = c.links.apply(s.links)
	return s
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
		n := moderation.Normalize(nil, t)
		if len(n) > 0 {
			out = append(out, n)
		}
	}
	return out
}

// disabled reports whether the module row is toggled off (floor-only mode).
func (c *Config) disabled() bool { return c != nil && c.Disabled }

// hasBlockTerms reports whether channel block-terms are in play (a disabled
// row's terms are not).
func (c *Config) hasBlockTerms() bool {
	return c != nil && !c.Disabled && len(c.blockTerms) > 0
}

// allows reports whether the skeleton contains a channel allow-term, which
// suppresses a non-floor flag (heuristic or channel block-term). Floor matches
// never consult this.
func (c *Config) allows(skel []byte) bool {
	if c == nil || c.Disabled {
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
