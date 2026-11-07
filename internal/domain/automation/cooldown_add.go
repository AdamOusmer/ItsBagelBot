// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"strconv"
	"strings"
)

const LongCooldownNamedPrefix = "cooldown:named:"

// LongCooldownNamedKey is the cooldown_claims key a named cooldown's window lives under.
func LongCooldownNamedKey(channel uint64, name string) string {
	return LongCooldownNamedPrefix + strconv.FormatUint(channel, 10) + ":" + strings.ToLower(name)
}

// IsNamedCooldownKey reports a window that stays on the long path no matter its length.
func IsNamedCooldownKey(key string) bool {
	return strings.HasPrefix(key, LongCooldownNamedPrefix)
}

const maxCooldownAddPer = 1_000_000

type CooldownAddAction struct {
	Name    string `json:"name"`
	Seconds int    `json:"seconds"`
	Per     int    `json:"per"`
}

func checkCooldownAdd(c chk, p CooldownAddAction) {
	c.variableName("name", p.Name)
	inRange(c, "seconds", p.Seconds, between(1, MaxCooldownSeconds))
	inRange(c, "per", p.Per, between(0, maxCooldownAddPer))
	if p.Per > 0 && !c.eventAmount {
		c.add("per", CodeTriggerMismatch)
	}
}

// triggerHasAmount reports a definition whose event trigger carries a numeric "amount" field, the only trigger shape cooldown_add's per can divide by.
func triggerHasAmount(t Trigger) bool {
	if t.Type != "event" {
		return false
	}
	p, err := DecodeParams[EventTrigger](t.Params)
	if err != nil {
		return false
	}
	kind, known := LookupEventKind(p.Kind)
	if !known {
		return false
	}
	f, ok := kind.field("amount")
	return ok && f.Numeric
}

// CooldownAddSeconds is the time cooldown_add adds for one event, with no remainder carried between events; ok is false below the rate.
func CooldownAddSeconds(p CooldownAddAction, amount int64) (int64, bool) {
	if p.Per <= 0 {
		return int64(p.Seconds), true
	}
	multiples := amount / int64(p.Per)
	if multiples <= 0 {
		return 0, false
	}
	return int64(p.Seconds) * multiples, true
}

// OwnedCooldownNames lists the names a definition arms by way of a cooldown condition, wherever it sits: conditions hold one at the top level and an if holds one in its condition.
func OwnedCooldownNames(def Definition) []string {
	var names []string
	collect := func(c Condition) {
		if c.Type != conditionCooldown {
			return
		}
		if p, err := DecodeParams[CooldownCondition](c.Params); err == nil && p.Name != "" {
			names = append(names, p.Name)
		}
	}
	for _, c := range def.Conditions {
		collect(c)
	}
	for _, a := range def.Flatten() {
		if a.Type != actionIf {
			continue
		}
		if p, err := DecodeParams[IfAction](a.Params); err == nil {
			collect(p.Condition)
		}
	}
	return names
}
