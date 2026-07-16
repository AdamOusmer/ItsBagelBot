package modules

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
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
type triggerWord struct {
	Phrase   string
	Response string
	Match    string
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
	var cfg triggersConfig
	if err := c.Decode(&cfg); err != nil {
		return err
	}
	line := triggerLine{text: text, user: strings.TrimPrefix(c.Env.ChatterName(), "@")}
	reply, ok := line.firstReply(cfg.rules())
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
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
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
		out = append(out, triggerWord{Phrase: phrase, Response: response, Match: normalizeMatch(r.Match)})
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
	return triggerWord{Phrase: phrase, Response: response, Match: mode}, true
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
	for _, tw := range rules {
		if !tw.matches(l) {
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

// matches reports whether tw fires on the line under its mode. The comparison is
// case-insensitive. An unknown mode is treated as "word".
func (tw triggerWord) matches(l triggerLine) bool {
	text := strings.ToLower(l.text)
	phrase := strings.ToLower(tw.Phrase)
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
