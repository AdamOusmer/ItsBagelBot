// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"math/rand/v2"
	"time"
)

const (
	backoffMin = 1 * time.Second
	backoffMax = 60 * time.Second
	stableFor  = 5 * time.Minute
)

type reconnect struct {
	attempt int
	draw    func(ceiling time.Duration) time.Duration
}

func newReconnect() *reconnect { return &reconnect{draw: fullJitter} }

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

func fullJitter(ceiling time.Duration) time.Duration {
	if ceiling <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(ceiling) + 1))
}
