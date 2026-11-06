// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"sort"
	"strconv"
	"unicode"
)

const matchRegex = "regex"

var (
	matchModes     = enum{"start", "exact", "anywhere", matchRegex}
	commandSources = enum{"chat", "sub_message"}
)

type CommandTrigger struct {
	Name          string   `json:"name"`
	Aliases       []string `json:"aliases"`
	Match         string   `json:"match"`
	CaseSensitive bool     `json:"case_sensitive"`
	Sources       []string `json:"sources"`
}

func (p *CommandTrigger) setDefaults() {
	p.Match = "start"
	p.Sources = []string{"chat"}
}

type EventTrigger struct {
	Kind    string            `json:"kind"`
	Filters map[string]string `json:"filters"`
}

type TimerTrigger struct {
	IntervalS     int  `json:"interval_s"`
	RandomJitterS int  `json:"random_jitter_s"`
	MinChatLines  int  `json:"min_chat_lines"`
	OfflineOK     bool `json:"offline_ok"`
}

type RewardTrigger struct {
	RewardID string `json:"reward_id"`
}

type EmptyParams struct{}

func checkEmpty(chk, EmptyParams) {}

type matchMode string

func checkCommand(c chk, p CommandTrigger) {
	c.oneOf("match", p.Match, matchModes)
	c.commandName("name", p.Name, matchMode(p.Match))
	c.aliases(p)
	if c.count("sources", quantity(len(p.Sources)), between(1, quantity(len(commandSources)))) {
		forEach("sources", p.Sources, c.eachOneOf(commandSources))
	}
}

func (c chk) commandName(f field, s string, mode matchMode) {
	switch mode {
	case matchRegex:
		c.regex(f, s, MaxPatternLength)
	case "anywhere":
		if c.text(f, s, nonEmpty(64)) {
			c.chars(f, s, unicode.IsPrint)
		}
	default:
		if c.text(f, s, nonEmpty(64)) {
			c.chars(f, s, isCommandChar)
		}
	}
}

func (c chk) aliases(p CommandTrigger) {
	if p.Match == matchRegex && len(p.Aliases) > 0 {
		c.add("aliases", CodeInvalidFormat)
		return
	}
	if c.count("aliases", quantity(len(p.Aliases)), upTo(MaxCommandAliases)) {
		forEach("aliases", p.Aliases, func(f field, s string) { c.commandName(f, s, matchMode(p.Match)) })
	}
}

type eventScope struct {
	kind  EventKind
	known bool
}

type filterEntry struct{ key, value string }

func checkEvent(c chk, p EventTrigger) {
	kind, known := LookupEventKind(p.Kind)
	if !known {
		c.eventKind(p.Kind)
	}
	c.count("filters", quantity(len(p.Filters)), upTo(MaxEventFilters))
	keys := make([]string, 0, len(p.Filters))
	for key := range p.Filters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	scope := eventScope{kind: kind, known: known}
	for _, key := range keys {
		c.filter(scope, filterEntry{key: key, value: p.Filters[key]})
	}
}

func (c chk) eventKind(kind string) {
	if kind == "" {
		c.add("kind", CodeRequired)
		return
	}
	c.add("kind", CodeUnknownEventKind)
}

func (c chk) filter(scope eventScope, e filterEntry) {
	name := field("filters." + e.key)
	c.text(name, e.value, upTo(MaxFilterValue))
	if !scope.known {
		return
	}
	f, ok := scope.kind.field(e.key)
	if !ok {
		c.add(name, CodeUnknownFilter)
		return
	}
	if _, err := strconv.ParseInt(e.value, 10, 64); f.Numeric && err != nil {
		c.add(name, CodeInvalidFormat)
	}
}

func checkTimer(c chk, p TimerTrigger) {
	inRange(c, "interval_s", p.IntervalS, between(60, 86400))
	inRange(c, "random_jitter_s", p.RandomJitterS, between(0, 3600))
	inRange(c, "min_chat_lines", p.MinChatLines, between(0, 100))
}

func checkReward(c chk, p RewardTrigger) { c.text("reward_id", p.RewardID, nonEmpty(64)) }
