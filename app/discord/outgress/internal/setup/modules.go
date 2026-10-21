// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"

	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

type GuildModuleWrite struct {
	GuildID         string
	BroadcasterID   string
	Row             ddiscord.ModuleRow
	ExpectedVersion int
}

func (w *Worker) GuildModules(ctx context.Context, req GuildSetupRequest) (ddiscord.GuildModules, bool, error) {
	if err := w.requireOwnerStrict(ctx, req, ownerCheck{}); err != nil {
		return ddiscord.GuildModules{}, false, err
	}
	mods, found := w.store.GuildModules(ctx, discordstore.Guild{ID: req.GuildID})
	return mods, found, nil
}

func (w *Worker) SetGuildModule(ctx context.Context, write GuildModuleWrite) (int, error) {
	req := GuildSetupRequest{GuildID: write.GuildID, BroadcasterID: write.BroadcasterID}
	if err := w.requireOwnerStrict(ctx, req, ownerCheck{}); err != nil {
		return 0, err
	}
	only := ddiscord.JoinModules(write.GuildID, []ddiscord.ModuleRow{write.Row})
	if err := w.requireConfigChannels(ctx, write.GuildID, only); err != nil {
		return 0, err
	}
	version, err := w.store.SetGuildModule(ctx, discordstore.SetModule{
		Guild:           discordstore.Guild{ID: write.GuildID},
		Broadcaster:     discordstore.Broadcaster{ID: write.BroadcasterID},
		Row:             write.Row,
		ExpectedVersion: write.ExpectedVersion,
	})
	if err != nil {
		return 0, err
	}
	w.store.Invalidate(ctx, discordstore.Guild{ID: write.GuildID})
	return version, nil
}
