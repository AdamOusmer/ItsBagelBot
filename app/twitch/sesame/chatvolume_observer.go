// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/chatvolume"
)

type chatVolumeObserver struct {
	store *chatvolume.Store
}

func (o chatVolumeObserver) Observe(ev engine.ObservedEvent) {
	o.store.Observe(chatvolume.Event{
		BroadcasterID: ev.BroadcasterID,
		Type:          ev.Type,
		At:            ev.At,
		Handled:       ev.Handled,
	})
}
