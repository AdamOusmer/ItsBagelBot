// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

import "slices"

// Mirrors web/kit/lib/catalog ids; a new module is uncounted until listed here.
var storedModules = map[string]bool{
	"accountage": true, "alerts": true, "automations": false, "cmd": true, "automod": true, "channelpoints": false, "clashroyale": false, "clip": true,
	"codm": false, "commercial": true, "counters": true, "duel": false, "emoteplay": false,
	"followage": true, "fortnite": false, "gamble": false, "game": true, "govee": false, "loyalty": false,
	"marker": true, "mcsr": false, "moderation": true, "personality": true, "queue": false, "quotes": false,
	"raffle": false, "shoutout": false, "songqueue": false, "stream": true, "system": false, "tags": true,
	"time": false, "timers": true, "title": true, "triggers": false, "uptime": true, "urchin": false,
	"valorant": false,
}

func ModuleEnabledByDefault(name string) bool { return storedModules[name] }

func ModuleNames() []string {
	names := make([]string, 0, len(storedModules))
	for name := range storedModules {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func KnownModule(name string) bool {
	_, ok := storedModules[name]
	return ok
}
