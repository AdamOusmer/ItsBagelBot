// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"regexp"
	"strings"
	"unicode"
)

type matchName struct{ text, lower string }

type MatchText struct {
	raw, lower string
	lowered    bool
}

func NewMatchText(raw string) MatchText { return MatchText{raw: raw} }

func (m *MatchText) folded() string {
	if !m.lowered {
		m.lower, m.lowered = strings.ToLower(m.raw), true
	}
	return m.lower
}

func (t *CompiledTrigger) compileMatch() {
	names := CommandNames(t.Command)
	t.allNames = make([]matchName, 0, len(names))
	t.scanNames = make([]matchName, 0, len(names))
	for _, name := range names {
		n := matchName{text: name, lower: strings.ToLower(name)}
		t.allNames = append(t.allNames, n)
		if !t.Command.DispatchRouted(name) {
			t.scanNames = append(t.scanNames, n)
		}
	}
	if t.Command.Match == matchRegex {
		t.regex = compileTriggerRegex(t.Command)
	}
}

func compileTriggerRegex(c CommandTrigger) *regexp.Regexp {
	pattern := c.Name
	if !c.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re
}

func (t *CompiledTrigger) MatchChat(text *MatchText) (args string, ok bool) {
	return t.matchNames(t.scanNames, text)
}

func (t *CompiledTrigger) MatchSubMessage(text *MatchText) (args string, ok bool) {
	return t.matchNames(t.allNames, text)
}

func (t *CompiledTrigger) matchNames(names []matchName, text *MatchText) (string, bool) {
	subject := matchSubject{text: text, fold: !t.Command.CaseSensitive}
	switch t.Command.Match {
	case matchRegex:
		return text.raw, t.regex != nil && t.regex.MatchString(text.raw)
	case "start":
		return subject.anyStart(names)
	case "exact":
		return "", subject.anyExact(names)
	case "anywhere":
		return "", subject.anyAnywhere(names)
	}
	return "", false
}

type matchSubject struct {
	text *MatchText
	fold bool
}

func (s matchSubject) anyStart(names []matchName) (string, bool) {
	for i := range names {
		if args, ok := s.start(names[i].text); ok {
			return args, true
		}
	}
	return "", false
}

func (s matchSubject) anyExact(names []matchName) bool {
	for i := range names {
		if s.exact(names[i].text) {
			return true
		}
	}
	return false
}

func (s matchSubject) anyAnywhere(names []matchName) bool {
	for i := range names {
		if s.anywhere(names[i]) {
			return true
		}
	}
	return false
}

func (s matchSubject) hasPrefix(text, prefix string) bool {
	if len(text) < len(prefix) {
		return false
	}
	if s.fold {
		return strings.EqualFold(text[:len(prefix)], prefix)
	}
	return text[:len(prefix)] == prefix
}

func (s matchSubject) start(name string) (string, bool) {
	text := strings.TrimSpace(s.text.raw)
	if !s.hasPrefix(text, name) {
		return "", false
	}
	rest := text[len(name):]
	if rest != "" && !unicode.IsSpace(rune(rest[0])) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

func (s matchSubject) exact(name string) bool {
	text := strings.TrimSpace(s.text.raw)
	if s.fold {
		return strings.EqualFold(text, name)
	}
	return text == name
}

func (s matchSubject) anywhere(name matchName) bool {
	if s.fold {
		return strings.Contains(s.text.folded(), name.lower)
	}
	return strings.Contains(s.text.raw, name.text)
}
