// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Values outside this catalog are dropped: it is the only guard keeping
// identifiers and free text out of the aggregate store.
package analytics

import (
	"regexp"
	"slices"
)

type Metric string

type Kind string

type Type uint8

const (
	Counter Type = iota + 1
	Gauge
	Distinct
)

type Window uint8

const (
	Day Window = 1 << iota
	Week
	Month
)

type Spec struct {
	Type    Type
	Dims    []Kind
	Windows Window
}

const (
	ChatMessages        Metric = "chat.messages"
	ChatAnswered        Metric = "chat.answered"
	CommandUses         Metric = "command.uses"
	ModuleEvents        Metric = "module.events"
	ModuleErrors        Metric = "module.errors"
	ModuleLatency       Metric = "module.latency"
	TimerFires          Metric = "timer.fires"
	DiscordInteractions Metric = "discord.interactions"
	DiscordEvents       Metric = "discord.events"
	OutgressFailures    Metric = "outgress.failures"
	Lifecycle           Metric = "lifecycle"
	OnboardingPreset    Metric = "onboarding.preset"
	ModuleToggles       Metric = "module.toggles"
	CustomCommands      Metric = "custom.commands"
	WebViews            Metric = "web.views"
	WebActions          Metric = "web.actions"
	LiveMinutes         Metric = "live.minutes"
	LivePeak            Metric = "live.peak"
	ChannelsActive      Metric = "channels.active"
	ModuleActive        Metric = "module.active"
)

const (
	KindModule    Kind = "module"
	KindCommand   Kind = "command"
	KindDiscord   Kind = "discord"
	KindErr       Kind = "err"
	KindBucket    Kind = "bucket"
	KindFail      Kind = "fail"
	KindLifecycle Kind = "lifecycle"
	KindPreset    Kind = "preset"
	KindToggle    Kind = "toggle"
	KindChange    Kind = "change"
	KindApp       Kind = "app"
	KindRoute     Kind = "route"
	KindAction    Kind = "action"
	KindOutcome   Kind = "outcome"
)

// CustomCommand stands in for every custom command name, which is streamer-written text.
const CustomCommand = "custom"

var catalog = map[Metric]Spec{
	ChatMessages:        {Type: Counter},
	ChatAnswered:        {Type: Counter},
	CommandUses:         {Type: Counter, Dims: []Kind{KindCommand}},
	ModuleEvents:        {Type: Counter, Dims: []Kind{KindModule}},
	ModuleErrors:        {Type: Counter, Dims: []Kind{KindModule, KindErr}},
	ModuleLatency:       {Type: Counter, Dims: []Kind{KindModule, KindBucket}},
	TimerFires:          {Type: Counter},
	DiscordInteractions: {Type: Counter, Dims: []Kind{KindDiscord}},
	DiscordEvents:       {Type: Counter, Dims: []Kind{KindDiscord}},
	OutgressFailures:    {Type: Counter, Dims: []Kind{KindFail}},
	Lifecycle:           {Type: Counter, Dims: []Kind{KindLifecycle}},
	OnboardingPreset:    {Type: Counter, Dims: []Kind{KindPreset}},
	ModuleToggles:       {Type: Counter, Dims: []Kind{KindModule, KindToggle}},
	CustomCommands:      {Type: Counter, Dims: []Kind{KindChange}},
	WebViews:            {Type: Counter, Dims: []Kind{KindApp, KindRoute}},
	WebActions:          {Type: Counter, Dims: []Kind{KindApp, KindAction, KindOutcome}},
	LiveMinutes:         {Type: Counter},
	LivePeak:            {Type: Gauge},
	ChannelsActive:      {Type: Distinct, Windows: Day | Week | Month},
	ModuleActive:        {Type: Distinct, Dims: []Kind{KindModule}, Windows: Day | Week},
}

var enums = map[Kind][]string{
	KindErr:       {"timeout", "upstream", "invalid", "ratelimited", "internal"},
	KindBucket:    {"lt10ms", "lt50ms", "lt250ms", "lt1s", "ge1s"},
	KindFail:      {"ratelimited", "banned", "unauthorized", "client", "server", "network"},
	KindLifecycle: {"signup", "onboarded", "bot_enabled", "bot_disabled", "revoked", "chat_banned", "account_deleted", "guild_bound", "guild_unbound", "guild_removed"},
	KindPreset:    {"start-new", "integrate", "quiet", "import", "songs"},
	KindToggle:    {"on", "off", "quick_off"},
	KindChange:    {"created", "deleted", "quick_deleted"},
	KindApp:       {"dashboard", "admin"},
	KindOutcome:   {"ok", "invalid", "failed", "denied"},
}

var shapes = map[Kind]*regexp.Regexp{
	KindModule:  regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`),
	KindCommand: regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`),
	KindDiscord: regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,63}$`),
	KindRoute:   regexp.MustCompile(`^/[A-Za-z0-9_()\[\]./-]{0,110}$`),
	KindAction:  regexp.MustCompile(`^/[A-Za-z0-9_()\[\]./-]{0,90}\?/[A-Za-z0-9_]{1,32}$`),
}

func Lookup(m Metric) (Spec, bool) {
	spec, ok := catalog[m]
	return spec, ok
}

func Metrics() []Metric {
	out := make([]Metric, 0, len(catalog))
	for m := range catalog {
		out = append(out, m)
	}
	slices.Sort(out)
	return out
}

func Enum(k Kind) []string { return slices.Clone(enums[k]) }

type Registry map[Kind]map[string]struct{}

func NewRegistry(names map[Kind][]string) Registry {
	reg := Registry{}
	for kind, values := range names {
		set := map[string]struct{}{}
		for _, v := range values {
			if ShapeOK(kind, v) {
				set[v] = struct{}{}
			}
		}
		reg[kind] = set
	}
	return reg
}

// A nil registry accepts any well-shaped registered value; only stores that
// never see raw input may pass nil.
func Valid(m Metric, t Type, dims []string, reg Registry) bool {
	spec, ok := catalog[m]
	if !ok || spec.Type != t || len(dims) != len(spec.Dims) {
		return false
	}
	for i, kind := range spec.Dims {
		if !valueOK(kind, dims[i], reg) {
			return false
		}
	}
	return true
}

func valueOK(kind Kind, v string, reg Registry) bool {
	if values, ok := enums[kind]; ok {
		return slices.Contains(values, v)
	}
	if !ShapeOK(kind, v) {
		return false
	}
	if reg == nil || kind == KindRoute || kind == KindAction {
		return true
	}
	_, ok := reg[kind][v]
	return ok || (kind == KindCommand && v == CustomCommand)
}

func ShapeOK(kind Kind, v string) bool {
	re, ok := shapes[kind]
	return ok && re.MatchString(v)
}
