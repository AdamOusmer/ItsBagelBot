// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strconv"

	"ItsBagelBot/internal/domain/automation"
)

const eventAmountField = "amount"

func (x *actionExec) eventAmount() int64 {
	raw, ok := x.run.Event[eventAmountField]
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func (x *actionExec) cooldownAdd(s step, p automation.CooldownAddAction) error {
	name, ok := automation.VariableName(p.Name)
	if !ok {
		return failure("bad_name")
	}
	seconds, ok := automation.CooldownAddSeconds(p, x.eventAmount())
	if !ok {
		return skipped("below_rate")
	}
	if x.p.automations.LongCooldowns == nil {
		return failure("no_long_cooldowns")
	}
	key := automation.LongCooldownNamedKey(x.run.BroadcasterID, name)
	return x.once(s, func() error { return x.extendNamedCooldown(key, seconds) })
}

func (x *actionExec) extendNamedCooldown(key string, seconds int64) error {
	expiresAtMS, found, capped, err := x.p.automations.LongCooldowns.ExtendLongCooldown(x.ctx, x.run.BroadcasterID, key, seconds)
	switch {
	case err != nil:
		return failure("store_error")
	case !found:
		return skipped("not_running")
	}
	x.run.CooldownEndMS = expiresAtMS
	if capped {
		return succeeded("capped")
	}
	return succeeded(strconv.FormatInt(seconds, 10) + "s")
}

func (x *actionExec) cooldownReset(s step, p automation.NamedTimerAction) error {
	name, ok := automation.VariableName(p.Name)
	store := x.p.automations.LongCooldowns
	switch {
	case !ok:
		return failure("bad_name")
	case store == nil:
		return failure("no_long_cooldowns")
	}
	return x.once(s, func() error {
		if err := store.ResetLongCooldown(x.ctx, NamedCooldownRef{BroadcasterID: x.run.BroadcasterID, Name: name}); err != nil {
			return failure("store_error")
		}
		return succeeded(name)
	})
}
