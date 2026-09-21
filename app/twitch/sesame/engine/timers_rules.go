// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "time"

// This file holds the gate and stop decisions from docs/specs/timer-conditions.md
// (D16) as pure functions over (timerDef, counts, now): no Valkey, no logger, no
// clock of their own. The store (timers_valkey.go) reads the counters the rules
// need, calls them, and acts on the answer, read, decide, act stays one
// direction, and every branch here is table-tested without VALKEY_TEST_ADDR.

// maxGateLines and maxFireCap ceiling the two new integer fields to the
// console's own range (spec §4, D12). The dashboard already clamps before
// saving; this is the same defensive re-check clampInterval already does for
// the interval, guarding a hand-crafted RPC call that skipped the console's
// own clamp rather than a broadcaster who used the form.
const (
	maxGateLines = 100
	maxFireCap   = 100
)

// clampGateLines floors and ceilings a configured chat-activity threshold.
// Both ends matter: a negative value (only reachable by a hand-crafted RPC
// call, never the console) must not make gatePasses' delta comparison always
// true, and a value above the console's range must not demand more lines than
// the dashboard would ever let a broadcaster ask for.
func clampGateLines(n int) int {
	switch {
	case n < 0:
		return 0
	case n > maxGateLines:
		return maxGateLines
	default:
		return n
	}
}

// clampFireCap is clampGateLines' twin for the fire cap. Kept as a separate
// function, not a shared helper parameterized on the ceiling: the two fields
// clamp to the same range today by coincidence (spec §4's table), and a
// shared helper would make a future change to one field's range silently
// reach the other.
func clampFireCap(n int) int {
	switch {
	case n < 0:
		return 0
	case n > maxFireCap:
		return maxFireCap
	default:
		return n
	}
}

// isGated reports whether td carries an active chat-activity gate. 0 (or
// anything clamping to 0) means off, matching D2/D11: a missing or zeroed
// field is indistinguishable from "this broadcaster never opened the field."
func isGated(td timerDef) bool {
	return clampGateLines(td.MinChatLines) > 0
}

// gatePasses reports whether a tick clears td's chat-activity gate (§6 step
// 2). No gate always passes. A gate passes when the chat-line counter has
// moved past the watermark by at least the threshold since the timer's last
// fire, or since it armed, for a first tick, because armAll/armJittered seed
// the watermark to the counter's value at arm time (D4, Q4). lines and mark
// are both read fresh by the caller; a skipped tick calls this again next
// expiry with the same mark and a larger lines, which is exactly how a gate
// is meant to accumulate activity across skips.
func gatePasses(td timerDef, lines, mark int64) bool {
	threshold := clampGateLines(td.MinChatLines)
	if threshold <= 0 {
		return true
	}
	return lines-mark >= int64(threshold)
}

// stopped reports whether td has permanently ended for this stream (§6 step
// 1): its fire cap was already reached, or now is at or past its end date.
// Either one is terminal, no fire, no re-arm, which is why they share one
// function instead of two call sites the caller could accidentally check in
// the wrong order.
//
// fires must come from a tick or arm-time read of the fire-count key, never
// incremented for a gate-skipped tick (D9): a skip must not count against the
// cap, so the caller is responsible for only counting fires that actually
// fired.
func stopped(td timerDef, fires int64, now time.Time) bool {
	if fireCap := clampFireCap(td.MaxFires); fireCap > 0 && fires >= int64(fireCap) {
		return true
	}
	endsAt, ok := parseEndsAt(td.EndsAt)
	return ok && !now.Before(endsAt)
}

// parseEndsAt decodes td.EndsAt as an RFC 3339 instant. ok is false for both
// an empty string (never ends, D6's default) and a value that fails to parse.
// D11 says a missing field means off, and a field the console never should
// have written unparsable degrades the same way rather than panicking a tick.
// The store, not this function, decides whether an unparsable-but-non-empty
// value is worth a log line (D16): a pure rule has no logger to write to, and
// folding that decision in here would make every call site that only cares
// about the bool also pay for (and mock) the logging path.
func parseEndsAt(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
