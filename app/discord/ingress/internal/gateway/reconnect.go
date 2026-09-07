// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"math/rand/v2"
	"time"
)

const (
	// backoffMin/backoffMax bound the reconnect wait. The old loop slept a
	// flat 2s forever, which is why a 4004 (bad token) turned into a
	// permanent 30-reconnects-a-minute hammer on Discord's identify budget.
	// 1s keeps an ordinary blip (Discord rolling a gateway node) invisible;
	// 60s is where a sustained outage settles without the pod ever giving
	// up on its own.
	backoffMin = 1 * time.Second
	backoffMax = 60 * time.Second
	// stableFor is how long a socket has to live before its death counts as
	// a fresh incident rather than a continuation. Discord's own session
	// window is minutes, and a socket that survived five of them was
	// healthy: resuming the previous attempt counter there would have a
	// bot that reconnects cleanly once an hour waiting the full 60s every
	// time.
	stableFor = 5 * time.Minute
)

// reconnect is the backoff state across one Session.Run. Full jitter (a
// uniform draw in [0, ceiling]) rather than the ceiling itself: ingress is a
// single replica today, but the fleet's other gateway clients reconnect off
// the same Discord outage, and undithered backoff makes them all knock at
// the same instant. See AWS's "Exponential Backoff and Jitter" -- full
// jitter minimises total contention at the cost of some very short waits,
// which is the right trade when the retry is one WebSocket dial.
type reconnect struct {
	attempt int
	// draw picks the actual wait inside [0, ceiling]. Tests replace it to
	// assert the ceiling schedule without depending on random draws.
	draw func(ceiling time.Duration) time.Duration
}

func newReconnect() *reconnect { return &reconnect{draw: fullJitter} }

// next reports the wait before the next dial. up is how long the socket that
// just died stayed connected; a socket that stayed up at least stableFor
// resets the schedule.
func (r *reconnect) next(up time.Duration) time.Duration {
	if up >= stableFor {
		r.attempt = 0
	}
	ceiling := backoffCeiling(r.attempt)
	r.attempt++
	if r.draw == nil {
		return ceiling
	}
	return r.draw(ceiling)
}

// backoffCeiling doubles backoffMin per attempt and clamps at backoffMax.
// The shift is guarded rather than clever: attempt is unbounded (a gateway
// can stay unreachable for days) and 1s << 63 wraps to a negative duration,
// which would turn the backoff into a busy loop.
func backoffCeiling(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	d := backoffMin
	for range attempt {
		d *= 2
		if d >= backoffMax {
			return backoffMax
		}
	}
	return d
}

// fullJitter draws uniformly from [0, ceiling].
func fullJitter(ceiling time.Duration) time.Duration {
	if ceiling <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(ceiling) + 1))
}
