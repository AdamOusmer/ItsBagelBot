// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"strings"
	"time"
)

// Domain types for the bench rig: the NATS lane a run drives and the
// wall-clock instants its payloads and windows carry.

type benchLane struct {
	url     string
	stream  string
	subject string
	group   string
}

// unixNano is a wall-clock instant in nanoseconds since the epoch, the form
// the bench's payloads and measurement windows carry.

// unixNano is a wall-clock instant in nanoseconds since the epoch, the form
// the bench's payloads and measurement windows carry.
type unixNano int64

// wait blocks until the instant arrives; a zero or past instant is immediate.

// wait blocks until this feeder's next group slot comes due. It sleeps once per
// `every` calls, to a slot that advances by stride*every, because a per-message
// sleep at sub-100µs strides is all timer granularity and no pacing — see
// feedPacer.
func (p *feedPacer) wait() {
	if !p.on {
		return
	}
	p.n++
	if p.n < p.every {
		return
	}
	p.n = 0
	p.slot = p.slot.Add(p.stride * time.Duration(p.every))
	if d := time.Until(p.slot); d > 0 {
		time.Sleep(d)
	}
}

// publishOne sends one message, confirmed (commit latency sampled) or raw.

func durableFor(lane benchLane) string {
	return lane.group + "_" + strings.NewReplacer(".", "_", "*", "_", ">", "_").Replace(lane.subject)
}

// deleteBenchConsumer removes the bench durable, tolerating its absence.

// wait blocks until the instant arrives; a zero or past instant is immediate.
func (t unixNano) wait() {
	if t <= 0 {
		return
	}
	if d := time.Until(time.Unix(0, int64(t))); d > 0 {
		time.Sleep(d)
	}
}
