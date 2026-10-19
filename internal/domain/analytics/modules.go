// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

// Stored module row names are client-supplied; only these reach the aggregate store.
// Mirrors web/kit/lib/catalog ids; a new module is uncounted until listed here.
var storedModules = map[string]struct{}{
	"accountage": {}, "alerts": {}, "automod": {}, "channelpoints": {}, "clashroyale": {}, "clip": {},
	"codm": {}, "commercial": {}, "counters": {}, "discord": {}, "duel": {}, "emoteplay": {},
	"followage": {}, "fortnite": {}, "gamble": {}, "game": {}, "govee": {}, "loyalty": {},
	"marker": {}, "mcsr": {}, "moderation": {}, "personality": {}, "queue": {}, "quotes": {},
	"raffle": {}, "shoutout": {}, "songqueue": {}, "stream": {}, "system": {}, "tags": {},
	"time": {}, "timers": {}, "title": {}, "triggers": {}, "uptime": {}, "urchin": {},
	"valorant": {},
}

func KnownModule(name string) bool {
	_, ok := storedModules[name]
	return ok
}
