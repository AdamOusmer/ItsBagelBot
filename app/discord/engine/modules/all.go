// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/internal/discordstore"

	"go.uber.org/zap"
)

// Deps is every collaborator a discord.ingress.event.*-driven module needs.
// Live and Clip are wired separately in main (see their own doc comments):
// they are driven off Twitch subjects, not a Discord gateway dispatch type,
// so they never go through the module.Builder/Registry at all.
type Deps struct {
	Store     discordstore.Store
	Channels  voiceClient
	Purge     purgeClient
	Guard     Guarder
	OwnInvite OwnInviteChecker
	Identity  *Identity
	Log       *zap.Logger
}

// All returns every module the dispatcher indexes, mirroring
// app/sesame/modules.All's role as the single assembly point.
func All(d Deps) []module.Module {
	return []module.Module{
		Welcome(),
		Message(d.Store),
		Rank(d.Store),
		Moderation(d.Purge, d.Log),
		Ticket(d.Store, d.Channels, d.Log),
		Voice(d.Store, d.Channels, d.Log),
		LinkGuard(d.Guard, d.OwnInvite, d.Log),
		IdentityModule(d.Identity),
	}
}
