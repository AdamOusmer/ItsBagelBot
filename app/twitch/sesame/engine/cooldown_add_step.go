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

// cooldownAdd extends a named cooldown's window while it runs. A refusal (the window is not running, or the event
// fell below the rate) costs nothing and never blocks a retry; a store failure is kept at most once instead.
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
	return x.onceRefusable(s, func() error { return x.extendNamedCooldown(key, seconds) })
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
