// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/internal/chatvolume"
)

// chatVolumeObserver adapts engine.ObservedEvent to chatvolume.Event so
// internal/chatvolume — an internal/* package — never imports
// app/sesame/engine (see chatvolume.Event's doc for why: internal/* stays
// below app/*, the same layering internal/activity keeps with its own Row
// type). This is the one place that translation happens.
//
// This is a SEPARATE Observer from activityObserver, registered at its own
// RegisterObserver call site in main.go: observe.go's registry is
// deliberately many-observers-wide so neither lane edits the pipeline to add
// its hook.
type chatVolumeObserver struct {
	store *chatvolume.Store
}

// Observe implements engine.Observer. It only forwards; chatvolume.Store.
// Observe does its own bounding (writeTimeout) so this never blocks past
// that, matching activityObserver's posture on the same observer goroutine.
func (o chatVolumeObserver) Observe(ev engine.ObservedEvent) {
	o.store.Observe(chatvolume.Event{
		BroadcasterID: ev.BroadcasterID,
		Type:          ev.Type,
		At:            ev.At,
		Handled:       ev.Handled,
	})
}
