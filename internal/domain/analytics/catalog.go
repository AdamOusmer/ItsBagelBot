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
	Distinct
	Level
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
	ChatMessages     Metric = "chat.messages"
	ChatAnswered     Metric = "chat.answered"
	CommandUses      Metric = "command.uses"
	CommandChannels  Metric = "command.channels"
	ModuleEvents     Metric = "module.events"
	TimerFires       Metric = "timer.fires"
	Lifecycle        Metric = "lifecycle"
	OnboardingPreset Metric = "onboarding.preset"
	ModuleToggles    Metric = "module.toggles"
	CustomCommands   Metric = "custom.commands"
	LiveStarts       Metric = "live.starts"
	ChannelsLive     Metric = "channels.live"
	ChannelsActive   Metric = "channels.active"
	ModuleActive     Metric = "module.active"
	CohortActive     Metric = "cohort.active"
	BillingEvents    Metric = "billing.events"

	AccountsTotal         Metric = "accounts.total"
	AccountsBotEnabled    Metric = "accounts.bot_enabled"
	AccountsOnboarded     Metric = "accounts.onboarded"
	AccountsPaidSource    Metric = "accounts.paid_source"
	AccountsCancelPending Metric = "accounts.cancel_pending"
	AccountsPaymentFailed Metric = "accounts.payment_failed"
	AccountsLocale        Metric = "accounts.locale"
	AccountsPagesHidden   Metric = "accounts.pages_hidden"
	AccountsBySignupWeek  Metric = "accounts.by_signup_week"
	PaidBySignupWeek      Metric = "accounts.paid_by_signup_week"

	ModulesRows           Metric = "modules.rows"
	ModulesBreadth        Metric = "modules.breadth"
	IntegrationsConnected Metric = "integrations.connected"
	CustomTotal           Metric = "custom.total"
	CustomPerm            Metric = "custom.perm"
	CustomFlags           Metric = "custom.flags"
	CustomChannels        Metric = "custom.channels"
)

const (
	KindModule    Kind = "module"
	KindCommand   Kind = "command"
	KindLifecycle Kind = "lifecycle"
	KindPreset    Kind = "preset"
	KindToggle    Kind = "toggle"
	KindChange    Kind = "change"
	KindWeek      Kind = "week"
	KindTier      Kind = "tier"
	KindSource    Kind = "source"
	KindLocale    Kind = "locale"
	KindBilling   Kind = "billing"
	KindPage      Kind = "page"
	KindState     Kind = "state"
	KindBreadth   Kind = "breadth"
	KindLink      Kind = "link"
	KindActive    Kind = "active"
	KindPerm      Kind = "perm"
	KindFlag      Kind = "flag"
)

// CustomCommand stands in for every custom command name, which is streamer-written text.
const CustomCommand = "custom"

var catalog = map[Metric]Spec{
	ChatMessages:     {Type: Counter},
	ChatAnswered:     {Type: Counter},
	CommandUses:      {Type: Counter, Dims: []Kind{KindCommand}},
	CommandChannels:  {Type: Distinct, Dims: []Kind{KindCommand}, Windows: Week},
	ModuleEvents:     {Type: Counter, Dims: []Kind{KindModule}},
	TimerFires:       {Type: Counter},
	Lifecycle:        {Type: Counter, Dims: []Kind{KindLifecycle}},
	OnboardingPreset: {Type: Counter, Dims: []Kind{KindPreset}},
	ModuleToggles:    {Type: Counter, Dims: []Kind{KindModule, KindToggle}},
	CustomCommands:   {Type: Counter, Dims: []Kind{KindChange}},
	LiveStarts:       {Type: Counter},
	ChannelsLive:     {Type: Distinct, Windows: Day | Week | Month},
	ChannelsActive:   {Type: Distinct, Windows: Day | Week | Month},
	ModuleActive:     {Type: Distinct, Dims: []Kind{KindModule}, Windows: Day | Week},
	CohortActive:     {Type: Distinct, Dims: []Kind{KindWeek}, Windows: Week},
	BillingEvents:    {Type: Counter, Dims: []Kind{KindBilling}},

	AccountsTotal:         {Type: Level, Dims: []Kind{KindTier}},
	AccountsBotEnabled:    {Type: Level, Dims: []Kind{KindTier}},
	AccountsOnboarded:     {Type: Level},
	AccountsPaidSource:    {Type: Level, Dims: []Kind{KindSource}},
	AccountsCancelPending: {Type: Level},
	AccountsPaymentFailed: {Type: Level},
	AccountsLocale:        {Type: Level, Dims: []Kind{KindLocale}},
	AccountsPagesHidden:   {Type: Level, Dims: []Kind{KindPage}},
	AccountsBySignupWeek:  {Type: Level, Dims: []Kind{KindWeek}},
	PaidBySignupWeek:      {Type: Level, Dims: []Kind{KindWeek}},

	ModulesRows:           {Type: Level, Dims: []Kind{KindModule, KindState}},
	ModulesBreadth:        {Type: Level, Dims: []Kind{KindBreadth}},
	IntegrationsConnected: {Type: Level, Dims: []Kind{KindLink}},
	CustomTotal:           {Type: Level, Dims: []Kind{KindActive}},
	CustomPerm:            {Type: Level, Dims: []Kind{KindPerm}},
	CustomFlags:           {Type: Level, Dims: []Kind{KindFlag}},
	CustomChannels:        {Type: Level},
}

var enums = map[Kind][]string{
	KindLifecycle: {"signup", "onboarded", "bot_enabled", "bot_disabled", "revoked", "chat_banned", "account_deleted"},
	KindPreset:    {"start-new", "integrate", "quiet", "import", "songs"},
	KindToggle:    {"on", "off"},
	KindChange:    {"created", "deleted"},
	KindTier:      {"free", "paid", "vip"},
	KindSource:    {"tebex", "admin", "giveaway", "vip"},
	KindBilling:   {"activate", "cancel_requested", "cancel_aborted", "revoke", "payment_failed", "expired"},
	KindPage:      {"commands", "leaderboard", "songqueue"},
	KindState:     {"on", "off"},
	KindLink:      {"spotify", "govee", "fetch_key"},
	KindActive:    {"active", "inactive"},
	KindPerm:      {"everyone", "sub", "vip", "mod", "lead_mod", "broadcaster"},
	KindFlag:      {"online_only", "aliased", "cooldown", "counter"},
}

var shapes = map[Kind]*regexp.Regexp{
	KindModule:  regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`),
	KindCommand: regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`),
	KindWeek:    regexp.MustCompile(`^[0-9]{4}-W[0-9]{2}$`),
	KindLocale:  regexp.MustCompile(`^[a-z]{2}(-[A-Za-z]{2,4})?$`),
	KindBreadth: regexp.MustCompile(`^[0-9]{1,3}$`),
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

func ParseWindow(code string) (Window, bool) {
	switch code {
	case "d":
		return Day, true
	case "w":
		return Week, true
	case "m":
		return Month, true
	}
	return 0, false
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
	if !ok || !spec.fits(t, dims) {
		return false
	}
	for i, kind := range spec.Dims {
		if !valueOK(kind, dims[i], reg) {
			return false
		}
	}
	return true
}

func (s Spec) fits(t Type, dims []string) bool {
	return s.Type == t && len(dims) == len(s.Dims)
}

func valueOK(kind Kind, v string, reg Registry) bool {
	if values, ok := enums[kind]; ok {
		return slices.Contains(values, v)
	}
	if kind == KindCommand && v == CustomCommand {
		return true
	}
	if reg != nil && registered(kind) {
		_, ok := reg[kind][v]
		return ok
	}
	return ShapeOK(kind, v)
}

func registered(kind Kind) bool {
	return kind == KindModule || kind == KindCommand
}

func ShapeOK(kind Kind, v string) bool {
	re, ok := shapes[kind]
	return ok && re.MatchString(v)
}
