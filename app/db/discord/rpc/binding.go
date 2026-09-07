// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/discord/repository"
	"ItsBagelBot/internal/domain/rpc/discorddata"
)

type bindingRPC struct{ repo BindingStore }

// subscribeBindings registers the four guild-binding verbs.
func subscribeBindings(w Wiring) error {
	h := bindingRPC{repo: w.Repo}
	return errors.Join(
		serve(w, discorddata.VerbBindingGet, h.get),
		serve(w, discorddata.VerbBindingSet, h.set),
		serve(w, discorddata.VerbBindingDelete, h.remove),
		serve(w, discorddata.VerbBindingListByBroadcaster, h.listByBroadcaster),
	)
}

func (h bindingRPC) get(ctx context.Context, req discorddata.BindingGetRequest) discorddata.BindingGetReply {
	broadcasterID, found, err := h.repo.BindingGet(ctx, req.GuildID)
	if err != nil {
		message, code := failure(err)
		return discorddata.BindingGetReply{Error: message, Code: code}
	}
	return discorddata.BindingGetReply{BroadcasterID: broadcasterID, Found: found}
}

func (h bindingRPC) set(ctx context.Context, req discorddata.BindingSetRequest) discorddata.BindingSetReply {
	err := h.repo.BindingSet(ctx, repository.BindParams{
		GuildID:       req.GuildID,
		BroadcasterID: req.BroadcasterID,
		InstalledBy:   req.InstalledBy,
	})
	message, code := failure(err)
	return discorddata.BindingSetReply{Error: message, Code: code}
}

func (h bindingRPC) remove(ctx context.Context, req discorddata.BindingDeleteRequest) discorddata.BindingDeleteReply {
	err := h.repo.BindingDelete(ctx, req.GuildID, req.BroadcasterID)
	message, code := failure(err)
	return discorddata.BindingDeleteReply{Error: message, Code: code}
}

func (h bindingRPC) listByBroadcaster(ctx context.Context, req discorddata.BindingListByBroadcasterRequest) discorddata.BindingListByBroadcasterReply {
	rows, err := h.repo.BindingListByBroadcaster(ctx, req.BroadcasterID)
	if err != nil {
		message, code := failure(err)
		return discorddata.BindingListByBroadcasterReply{Error: message, Code: code}
	}
	guilds := make([]discorddata.Binding, 0, len(rows))
	for _, row := range rows {
		guilds = append(guilds, discorddata.Binding{
			GuildID:       row.GuildID,
			BoundAtUnixMs: unixMs(row.BoundAt),
			InstalledBy:   row.InstalledBy,
		})
	}
	return discorddata.BindingListByBroadcasterReply{Guilds: guilds}
}
