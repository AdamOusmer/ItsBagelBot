// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/modules/repository"
	riotrpc "ItsBagelBot/internal/domain/rpc/riot"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/env/conf"
)

// Only the dashboard's sign-in callback may set a link; a link is the player's consent to be shown.
func wireRiot(w bus.RPCWiring, links *repository.RiotLinks) error {
	dash := env.Get("NATS_MODULES_RIOT_SUBJECT_PREFIX", "rpc.modules.riot.link")
	internal := conf.RiotLinkPrefix.Get()
	r := &riotRPC{links: links}
	return errors.Join(
		bus.ServeForUser[riotrpc.LinkSetRequest, riotrpc.LinkMutateReply](w, dash+".set", r.handleSet),
		bus.ServeForUser[riotrpc.LinkUserRequest, riotrpc.LinkMutateReply](w, dash+".clear", r.handleClear),
		bus.ServeForUser[riotrpc.LinkUserRequest, riotrpc.LinkReply](w, dash+".status", r.handleGet),
		bus.ServeForUser[riotrpc.LinkUserRequest, riotrpc.LinkReply](w, internal+".get", r.handleGet),
	)
}

type riotRPC struct {
	links *repository.RiotLinks
}

func (r *riotRPC) handleSet(ctx context.Context, req riotrpc.LinkSetRequest, id uint64) (riotrpc.LinkMutateReply, error) {
	return riotrpc.LinkMutateReply{}, r.links.Set(ctx, id, repository.RiotLink{
		Puuid: req.Puuid, GameName: req.GameName, TagLine: req.TagLine, Shard: req.Shard,
	})
}

func (r *riotRPC) handleClear(ctx context.Context, _ riotrpc.LinkUserRequest, id uint64) (riotrpc.LinkMutateReply, error) {
	return riotrpc.LinkMutateReply{}, r.links.Clear(ctx, id)
}

func (r *riotRPC) handleGet(ctx context.Context, _ riotrpc.LinkUserRequest, id uint64) (riotrpc.LinkReply, error) {
	link, ok, err := r.links.Get(ctx, id)
	if err != nil || !ok {
		return riotrpc.LinkReply{}, err
	}
	return riotrpc.LinkReply{
		Linked:   true,
		LinkedAt: link.LinkedAt.Unix(),
		Link:     riotrpc.Link{Puuid: link.Puuid, GameName: link.GameName, TagLine: link.TagLine, Shard: link.Shard},
	}, nil
}
