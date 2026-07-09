package modules

import (
	"context"
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

// triggersConfig is the broadcaster's trigger-word rule set, read from the
// module's Configs blob (the pipeline wires it into the Context).
type triggersConfig struct {
	Triggers []triggerWord `json:"triggers"`
}

// triggerWord is one phrase→response rule.
//
//   - Phrase is the text to look for in a chat message.
//   - Response is the chat line the bot posts on a match. It supports the same
//     {user}/{random}/{choice:…} tokens custom commands do.
//   - Match selects how Phrase is compared: "word" (default, whole-word match so
//     "hi" does not fire on "this"), "contains" (anywhere as a substring),
//     "exact" (the whole message equals the phrase), or "prefix" (the message
//     starts with the phrase). Unknown values fall back to "word".
//   - CaseSensitive keeps the comparison case-exact; by default it is folded.
type triggerWord struct {
	Phrase        string `json:"phrase"`
	Response      string `json:"response"`
	Match         string `json:"match"`
	CaseSensitive bool   `json:"case_sensitive"`
}

// Triggers is the trigger-words module: it watches ordinary chat and, when a
// message contains one of the broadcaster's configured phrases, posts the paired
// response — no "!" command needed. It is a named, opt-in module (KindOptIn): off
// by default, enabled and configured per channel from the dashboard.
//
// The handler runs on the non-command chat path, so it fires on plain messages.
// Reaching it depends on ingress passing non-"!" chat through (the
// chat_passthrough lane gate); premium/special-user chat always flows.
//
// Command lines (a leading "!") and folded duplicate cohorts are skipped, and
// the first matching rule wins so one message yields at most one reply.
func Triggers(_ engine.Deps) module.Module {
	m := module.NewModule("triggers", module.KindOptIn)

	m.On("channel.chat.message", func(_ context.Context, c *module.Context, emit module.Emit) error {
		// Cohorts are folded duplicate spam, and a "!"-prefixed line is a command
		// the dispatcher owns; neither is a trigger-word candidate.
		text := strings.TrimSpace(c.Env.Text)
		if text == "" || len(c.Env.Senders) > 0 || strings.HasPrefix(text, "!") {
			return nil
		}

		var cfg triggersConfig
		if err := c.Decode(&cfg); err != nil {
			return err
		}
		if len(cfg.Triggers) == 0 {
			return nil
		}

		for i, tw := range cfg.Triggers {
			if i >= maxTriggers {
				break
			}
			if tw.Phrase == "" || tw.Response == "" {
				continue
			}
			if !matchTrigger(text, tw.Phrase, tw.Match, tw.CaseSensitive) {
				continue
			}

			user := strings.TrimPrefix(c.Env.ChatterName(), "@")
			msg := module.ExpandString(tw.Response, func(key string) (string, bool) {
				if key == "user" {
					return user, true
				}
				return module.ParseDynamic(key)
			})
			if msg == "" {
				return nil
			}
			emit(&module.Output{
				Type:          outgress.TypeChat,
				BroadcasterID: c.Env.BroadcasterUserID,
				Text:          msg,
			})
			return nil
		}
		return nil
	})

	return m.Build()
}

// matchTrigger reports whether text satisfies the phrase under the given match
// mode. Comparisons fold case unless caseSensitive is set. An unknown mode is
// treated as "word".
func matchTrigger(text, phrase, mode string, caseSensitive bool) bool {
	if !caseSensitive {
		text = strings.ToLower(text)
		phrase = strings.ToLower(phrase)
	}
	switch strings.ToLower(mode) {
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

// wordEdge reports whether position idx in s is a word boundary: the start or end
// of s, or a spot where the adjacent rune is not alphanumeric. idx is the byte
// index just before the rune that follows the boundary (0 and len(s) are edges).
func wordEdge(s string, idx int) bool {
	if idx <= 0 || idx >= len(s) {
		return true
	}
	before, _ := utf8.DecodeLastRuneInString(s[:idx])
	after, _ := utf8.DecodeRuneInString(s[idx:])
	return !isWordRune(before) || !isWordRune(after)
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
