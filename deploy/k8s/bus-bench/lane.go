// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"strings"
	"time"
)

type benchLane struct {
	url     string
	stream  string
	subject string
	group   string
}

type unixNano int64

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

func durableFor(lane benchLane) string {
	return lane.group + "_" + strings.NewReplacer(".", "_", "*", "_", ">", "_").Replace(lane.subject)
}

func (t unixNano) wait() {
	if t <= 0 {
		return
	}
	if d := time.Until(time.Unix(0, int64(t))); d > 0 {
		time.Sleep(d)
	}
}
