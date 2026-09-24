// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/internal/discordstore"

	"go.uber.org/zap"
)

type Deps struct {
	Store     discordstore.Store
	Channels  voiceClient
	Tickets   ticketClient
	Purge     purgeClient
	Guard     Guarder
	OwnInvite OwnInviteChecker
	Identity  *Identity
	Log       *zap.Logger
}

func All(d Deps) []module.Module {
	return []module.Module{
		Welcome(),
		Message(d.Store),
		Rank(d.Store),
		Moderation(d.Purge, d.Log),
		Ticket(d.Store, d.Tickets, d.Log),
		Voice(d.Store, d.Channels, d.Log),
		LinkGuard(d.Guard, d.OwnInvite, d.Log),
		IdentityModule(d.Identity),
	}
}
