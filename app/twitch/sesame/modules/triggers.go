// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"ItsBagelBot/app/twitch/sesame/confcache"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

// maxTriggers caps how many trigger rules the module evaluates per message. A
// broadcaster's config could be arbitrarily long; the scan is O(triggers ×
// message length) on the hot chat path, so a ceiling keeps a runaway config from
// slowing every line. Extra rules past the cap are ignored.
const maxTriggers = 50

// triggersConfig is the broadcaster's trigger-word config, read from the module's
// Configs blob (the pipeline wires it into the Context). Rules is the raw
// dashboard textarea: one "phrase => response" rule per line (see rules).
type triggersConfig struct {
	Rules string `json:"rules"`
}

// triggerWord is one parsed phrase→response rule. Match is the comparison mode
// (word/contains/exact/prefix); Phrase and Response are already trimmed.
//
// lower is Phrase pre-lowercased at parse time. Matching is case-insensitive and
// runs up to maxTriggers (50) times per chat line, so lowercasing the phrase
// inside matches() meant 50 throwaway allocations on every message for a value
// that never changes between them. Phrase itself is kept verbatim: it is what
// the dashboard round-trips, and callers other than matching read it.
type triggerWord struct {
	Phrase   string
	Response string
	Match    string
	lower    string
}

// newTriggerWord is the only way a rule is built, so lower can never drift from
// Phrase. Both parse paths (JSON and legacy lines) go through it.
func newTriggerWord(phrase, response, match string) triggerWord {
	return triggerWord{
		Phrase:   phrase,
		Response: response,
		Match:    match,
		lower:    strings.ToLower(phrase),
	}
}

// triggerLine is one chat message under evaluation: its trimmed text plus the
// chatter display name that fills {user} in a response. Bundling the two keeps
// the matchers method-shaped instead of threading raw strings everywhere.
type triggerLine struct {
	text string
	user string
}

// Triggers is the trigger-words module: it watches ordinary chat and, when a
// message matches one of the broadcaster's configured phrases, posts the paired
// response — no "!" command needed. It is a named, opt-in module (KindOptIn): off
// by default, enabled and configured per channel from the dashboard.
//
// The handler runs on the non-command chat path, so it fires on plain messages.
// Ingress forwards every chat line for every channel; identical spam arrives
// folded as a senders cohort, which triggerCandidate skips.
func Triggers(_ engine.Deps) module.Module {
	m := module.NewModule("triggers", module.KindOptIn)
	m.On("channel.chat.message", triggersOnChat)
	return m.Build()
}

// triggersOnChat is the chat handler: it screens the line, parses the rules, and
// emits the first matching rule's response (at most one reply per message).
func triggersOnChat(_ context.Context, c *module.Context, emit module.Emit) error {
	text, ok := triggerCandidate(c)
	if !ok {
		return nil
	}
	parsed := triggerRules.Get(c.Config, parseTriggerRules)
	if parsed.err != nil {
		return parsed.err
	}
	line := triggerLine{text: text, user: strings.TrimPrefix(c.Env.ChatterName(), "@")}
	reply, ok := line.firstReply(parsed.rules)
	if !ok {
		return nil
	}
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          reply,
	})
	return nil
}

// triggerRules memoizes the config blob -> rules parse per blob. rules() ran on
// EVERY chat line of every channel with the module on; the JSON decode plus the
// per-rule trimming and lowercasing is pure in its input, and that input changes
// only when the broadcaster saves the dashboard. Measured on an M1 Pro
// (2026-09-03, BenchmarkTriggersOnChatMiss, a full 50-rule config): 6380 ->
// 600 ns/op, 20KB -> 0 B/op, 13 -> 0 allocs per non-matching chat line, and a
// non-matching line is nearly every line. Package-level rather than
// captured per Triggers() instance because the cache key is the blob's content,
// which has no per-channel and no per-registration scope: one process building
// the module twice would otherwise parse the same bytes twice.
var triggerRules = confcache.New[parsedTriggers]()

// parsedTriggers is the cached parse. It carries the decode error alongside the
// rules because triggersOnChat used to return c.Decode's error to the engine
// (logged as a handler error, never nacked) and swallowing it here would be a
// silent behaviour change. The error is a pure function of the same bytes, so
// it caches exactly as soundly as the rules do.
//
// Both fields are READ-ONLY for callers: the slice is shared by every message
// on every channel using this blob, and firstReply only ranges it by value.
type parsedTriggers struct {
	rules []triggerWord
	err   error
}

// parseTriggerRules is the cache's parse function: decode the blob, then build
// the rules. An empty blob decodes to the zero config, exactly as Context.Decode
// does, so an unconfigured channel still yields no rules and no error.
func parseTriggerRules(raw []byte) parsedTriggers {
	if len(raw) == 0 {
		return parsedTriggers{}
	}
	var cfg triggersConfig
	if err := codec.Unmarshal(raw, &cfg); err != nil {
		return parsedTriggers{err: err}
	}
	return parsedTriggers{rules: cfg.rules()}
}

// triggerCandidate returns the trimmed chat text and whether the line is eligible
// for trigger matching: non-empty, not a folded duplicate cohort (Senders
// present), and not a "!" command (the dispatcher owns those).
func triggerCandidate(c *module.Context) (string, bool) {
	text := strings.TrimSpace(c.Env.Text)
	switch {
	case text == "":
		return "", false
	case len(c.Env.Senders) > 0:
		return "", false
	case strings.HasPrefix(text, "!"):
		return "", false
	default:
		return text, true
	}
}

// triggerRuleJSON is the structured on-disk form of a rule. The dashboard now
// writes config.rules as a JSON array of these, which — unlike the legacy
// "[mode:] phrase => response" line format — can carry a phrase containing "=>",
// a leading "#", or a "mode:" prefix without corrupting the round trip. rules()
// still reads the legacy line format for any config saved before the migration.
type triggerRuleJSON struct {
	Phrase   string `json:"phrase"`
	Response string `json:"response"`
	Match    string `json:"match"`
	// Pointer so an absent flag defaults to enabled (older writers omitted it).
	Enabled *bool `json:"enabled"`
}

// rules turns the stored config.rules into trigger rules. A value beginning with
// "[" is the structured JSON array; anything else is the legacy line format:
//
//	hello => hi {user}!
//	contains: lol => lmao
//
// A legacy line is "[mode:] phrase => response". Blank lines, "#" comments, lines
// without "=>", and lines with an empty phrase or response are skipped. At most
// maxTriggers rules are returned in either format.
//
// This no longer runs per chat message: triggersOnChat resolves the rules
// through the confcache above, so this runs once per distinct config blob.
// Indexing the exact-match rules into a map[string]triggerWord is still not
// worth it — the scan is already capped at maxTriggers and now happens off the
// per-message path entirely. Keying that cache on ModuleView.Revision, which an
// earlier note here proposed, was REJECTED: revision is 0 for legacy rows, so
// two different configs would collide on it and one channel would answer with
// another channel's triggers; the cache keys on the blob's content instead.
func (cfg triggersConfig) rules() []triggerWord {
	s := strings.TrimSpace(cfg.Rules)
	if s == "" {
		return nil
	}
	if s[0] == '[' {
		return jsonRules(s)
	}
	var out []triggerWord
	for _, ln := range strings.Split(cfg.Rules, "\n") {
		tw, ok := parseRuleLine(ln)
		if !ok {
			continue
		}
		out = append(out, tw)
		if len(out) >= maxTriggers {
			break
		}
	}
	return out
}

// jsonRules decodes the structured rule array. Malformed JSON yields no rules
// (fail closed, same as an empty config). Disabled rules and rules with an empty
// phrase or response are skipped; the match mode is normalised to a known value.
func jsonRules(s string) []triggerWord {
	var raw []triggerRuleJSON
	if err := codec.Unmarshal([]byte(s), &raw); err != nil {
		return nil
	}
	var out []triggerWord
	for _, r := range raw {
		if r.Enabled != nil && !*r.Enabled {
			continue
		}
		phrase := strings.TrimSpace(r.Phrase)
		response := strings.TrimSpace(r.Response)
		if phrase == "" || response == "" {
			continue
		}
		out = append(out, newTriggerWord(phrase, response, normalizeMatch(r.Match)))
		if len(out) >= maxTriggers {
			break
		}
	}
	return out
}

// normalizeMatch coerces a stored match mode into one the matchers understand,
// defaulting unknown/blank to "word".
func normalizeMatch(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "contains", "exact", "prefix":
		return strings.ToLower(strings.TrimSpace(m))
	default:
		return "word"
	}
}

// parseRuleLine parses one textarea line into a rule, reporting ok=false for a
// blank line, a comment, a line with no "=>", or an empty phrase/response.
func parseRuleLine(ln string) (triggerWord, bool) {
	ln = strings.TrimSpace(ln)
	if ln == "" || strings.HasPrefix(ln, "#") {
		return triggerWord{}, false
	}
	left, response, ok := strings.Cut(ln, "=>")
	if !ok {
		return triggerWord{}, false
	}
	mode, phrase := splitMode(strings.TrimSpace(left))
	response = strings.TrimSpace(response)
	if phrase == "" || response == "" {
		return triggerWord{}, false
	}
	return newTriggerWord(phrase, response, mode), true
}

// splitMode peels an optional "mode:" prefix (word/contains/exact/prefix) off a
// phrase. An unknown or absent prefix yields the default "word" mode with the
// phrase left unchanged.
func splitMode(phrase string) (mode, rest string) {
	pre, after, ok := strings.Cut(phrase, ":")
	if !ok {
		return "word", phrase
	}
	switch strings.ToLower(strings.TrimSpace(pre)) {
	case "word", "contains", "exact", "prefix":
		return strings.ToLower(strings.TrimSpace(pre)), strings.TrimSpace(after)
	default:
		return "word", phrase
	}
}

// firstReply returns the expanded response of the first rule that matches the
// line, or ok=false when none do. {user} resolves to the chatter name; {random}
// and {choice:…} resolve through the shared dynamic vars.
func (l triggerLine) firstReply(rules []triggerWord) (string, bool) {
	// Lowercased once for the whole scan rather than once per rule: the loop
	// runs up to maxTriggers (50) times per chat message and every mode wants
	// the same case-folded text, so the old per-rule strings.ToLower(l.text)
	// allocated a full copy of the message 50 times over for no gain.
	text := strings.ToLower(l.text)
	for _, tw := range rules {
		if !tw.matches(text) {
			continue
		}
		msg := module.ExpandString(tw.Response, func(key string) (string, bool) {
			if key == "user" {
				return l.user, true
			}
			return module.ParseDynamic(key)
		})
		if msg == "" {
			return "", false
		}
		return msg, true
	}
	return "", false
}

// matches reports whether tw fires on text under its mode. text is the chat line
// already lowercased by the caller and tw.lower is the phrase lowercased at parse
// time, which is what makes the comparison case-insensitive without either side
// being re-folded per rule. An unknown mode is treated as "word".
func (tw triggerWord) matches(text string) bool {
	phrase := tw.lower
	switch tw.Match {
	case "contains":
		return strings.Contains(text, phrase)
	case "exact":
		return text == phrase
	case "prefix":
		return strings.HasPrefix(text, phrase)
	default: // "word"
		return containsWord(text, phrase)
	}
}

// containsWord reports whether needle occurs in hay bounded by word edges, so
// "hi" matches "oh hi there" and "hi!" but not "this" or "high". A word edge is
// the start/end of the string or a non-alphanumeric rune. needle may itself hold
// spaces (a multi-word phrase); only its outer edges are checked.
func containsWord(hay, needle string) bool {
	if needle == "" {
		return false
	}
	for from := 0; from <= len(hay)-len(needle); {
		i := strings.Index(hay[from:], needle)
		if i < 0 {
			return false
		}
		start := from + i
		if wordEdge(hay, start) && wordEdge(hay, start+len(needle)) {
			return true
		}
		from = start + 1
	}
	return false
}

// wordEdge reports whether byte position idx in s is a word boundary: the start
// or end of s, or a spot where the rune on either side is not alphanumeric.
func wordEdge(s string, idx int) bool {
	if idx <= 0 || idx >= len(s) {
		return true
	}
	before, _ := utf8.DecodeLastRuneInString(s[:idx])
	after, _ := utf8.DecodeRuneInString(s[idx:])
	return !isWordRune(before) || !isWordRune(after)
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
