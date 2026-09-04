// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/discord/repository"
	discorddata "ItsBagelBot/internal/domain/rpc/discorddata"
	"ItsBagelBot/pkg/bus"
)

type bindingRPC struct{ repo BindingStore }

// subscribeBindings registers the four guild-binding verbs.
func subscribeBindings(w Wiring) error {
	h := bindingRPC{repo: w.Repo}
	if err := bus.QueueSubscribeJSON[discorddata.BindingGetRequest, discorddata.BindingGetReply](
		w.NC, w.subject(discorddata.VerbBindingGet), w.QueueGroup, requestTimeout, w.App, w.Log, h.get); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discorddata.BindingSetRequest, discorddata.BindingSetReply](
		w.NC, w.subject(discorddata.VerbBindingSet), w.QueueGroup, requestTimeout, w.App, w.Log, h.set); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discorddata.BindingDeleteRequest, discorddata.BindingDeleteReply](
		w.NC, w.subject(discorddata.VerbBindingDelete), w.QueueGroup, requestTimeout, w.App, w.Log, h.remove); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[discorddata.BindingByBroadcasterRequest, discorddata.BindingByBroadcasterReply](
		w.NC, w.subject(discorddata.VerbBindingByBroadcaster), w.QueueGroup, requestTimeout, w.App, w.Log, h.byBroadcaster)
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

func (h bindingRPC) byBroadcaster(ctx context.Context, req discorddata.BindingByBroadcasterRequest) discorddata.BindingByBroadcasterReply {
	guildID, found, err := h.repo.BindingByBroadcaster(ctx, req.BroadcasterID)
	if err != nil {
		message, code := failure(err)
		return discorddata.BindingByBroadcasterReply{Error: message, Code: code}
	}
	return discorddata.BindingByBroadcasterReply{GuildID: guildID, Found: found}
}
