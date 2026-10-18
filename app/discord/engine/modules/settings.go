// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func factSettings[T any](mods ddiscord.GuildModules, name string) (T, bool) {
	var s T
	if mods.GuildID == "" || !mods.On(name) {
		return s, false
	}
	return s, mods.Decode(name, &s) == nil
}

func settingsOf[T any](c *module.Context) (T, error) {
	var s T
	err := c.Decode(&s)
	return s, err
}

func peerOf[T any](c *module.Context, name string) T {
	var s T
	_ = c.Peer(name, &s)
	return s
}
