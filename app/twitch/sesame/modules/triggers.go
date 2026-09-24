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

const maxTriggers = 50

type triggersConfig struct {
	Rules string `json:"rules"`
}

type triggerWord struct {
	Phrase   string
	Response string
	Match    string
	lower    string
}

func newTriggerWord(phrase, response, match string) triggerWord {
	return triggerWord{
		Phrase:   phrase,
		Response: response,
		Match:    match,
		lower:    strings.ToLower(phrase),
	}
}

type triggerLine struct {
	text   string
	user   string
	locale string
}

func Triggers(_ engine.Deps) module.Module {
	m := module.NewModule("triggers", module.KindOptIn)
	m.On("channel.chat.message", triggersOnChat)
	return m.Build()
}

func triggersOnChat(_ context.Context, c *module.Context, emit module.Emit) error {
	text, ok := triggerCandidate(c)
	if !ok {
		return nil
	}
	parsed := triggerRules.Get(c.Config, parseTriggerRules)
	if parsed.err != nil {
		return parsed.err
	}
	line := triggerLine{text: text, user: strings.TrimPrefix(c.Env.ChatterName(), "@"), locale: c.Locale}
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

var triggerRules = confcache.New[parsedTriggers]()

type parsedTriggers struct {
	rules []triggerWord
	err   error
}

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

type triggerRuleJSON struct {
	Phrase   string `json:"phrase"`
	Response string `json:"response"`
	Match    string `json:"match"`
	Enabled  *bool  `json:"enabled"`
}

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

func normalizeMatch(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "contains", "exact", "prefix":
		return strings.ToLower(strings.TrimSpace(m))
	default:
		return "word"
	}
}

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

func (l triggerLine) firstReply(rules []triggerWord) (string, bool) {
	text := strings.ToLower(l.text)
	for _, tw := range rules {
		if !tw.matches(text) {
			continue
		}
		msg := module.KV("user", l.user).WithLocale(module.Locale(l.locale)).ExpandString(tw.Response)
		if msg == "" {
			return "", false
		}
		return msg, true
	}
	return "", false
}

func (tw triggerWord) matches(text string) bool {
	phrase := tw.lower
	switch tw.Match {
	case "contains":
		return strings.Contains(text, phrase)
	case "exact":
		return text == phrase
	case "prefix":
		return strings.HasPrefix(text, phrase)
	default:
		return containsWord(text, phrase)
	}
}

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

func wordEdge(s string, idx int) bool {
	if idx <= 0 || idx >= len(s) {
		return true
	}
	before, _ := utf8.DecodeLastRuneInString(s[:idx])
	after, _ := utf8.DecodeRuneInString(s[idx:])
	return !isWordRune(before) || !isWordRune(after)
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
