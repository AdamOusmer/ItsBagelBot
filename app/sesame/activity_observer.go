// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"strconv"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/internal/activity"
)

// activityObserver turns a handled command dispatch into the Overview
// feed's KindCommand row ("!bagel answered @novaburst", meta "41ms") and, by
// routing DurationMS through activity.Emit, feeds the feed's latency
// reservoir too (see internal/activity/store.go's median doc) — there is no
// separate latency-tracking path to keep in sync.
//
// This is a SEPARATE Observer from whatever the chat-volume lane registers
// at the same RegisterObserver call site in main.go: observe.go's registry
// is deliberately many-observers-wide (see its package doc) so neither lane
// edits the pipeline to add its hook.
type activityObserver struct{}

// Observe implements engine.Observer. It is intentionally cheap and never
// blocks past activity.Emit's own bounded write: this runs on observe.go's
// one goroutine per observer, and a slow Observe here would back up every
// later event for this observer specifically (see observe.go's ordering
// doc), not just this one.
func (activityObserver) Observe(ev engine.ObservedEvent) {
	if !ev.Handled || ev.Command == "" {
		return
	}
	row := activity.Row{
		Kind:       activity.KindCommand,
		Text:       "!" + ev.Command + " answered @" + ev.Actor,
		Meta:       strconv.Itoa(ev.DurationMS) + "ms",
		At:         ev.At,
		DurationMS: ev.DurationMS,
	}
	activity.Emit(context.Background(), strconv.FormatUint(ev.BroadcasterID, 10), row)
}
