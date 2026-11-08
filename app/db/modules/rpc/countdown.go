// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package rpc

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/app/db/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"
)

func SubscribeCountdown(w bus.RPCWiring, repo *repository.RecipeState, prefix string) error {
	s := &recipeStateRPC{repo: repo, log: w.Log}
	verb := func(name string) bus.Verb[modulesrpc.CountdownRequest, modulesrpc.CountdownReply] {
		return bus.VerbForUser[modulesrpc.CountdownRequest, modulesrpc.CountdownReply](name, func(ctx context.Context, req modulesrpc.CountdownRequest, id uint64) (modulesrpc.CountdownReply, error) {
			var result repository.CountdownResult
			var err error
			if name == "peek" {
				result, err = repo.CountdownPeek(ctx, id, req.Name)
			} else {
				result, err = repo.CountdownWrite(ctx, id, req.Name, name, req.Seconds, req.OperationID)
			}
			if errors.Is(err, repository.ErrVariableLimit) {
				return modulesrpc.CountdownReply{LimitExceeded: true}, nil
			}
			if err != nil {
				return modulesrpc.CountdownReply{Refusal: s.refusal("countdown."+name, err)}, nil
			}
			return modulesrpc.CountdownReply{Found: result.Found, Capped: result.Capped, ExpiresAtMS: result.ExpiresAtMS}, nil
		})
	}
	if err := bus.ServeVerbs(w, prefix+".countdown", verb("start"), verb("add"), verb("peek")); err != nil {
		return err
	}
	if err := bus.ServeVerbs(w, prefix+".countdown",
		bus.At("claim", func(ctx context.Context, _ modulesrpc.CountdownDeliveryRequest) modulesrpc.CountdownDeliveryReply {
			d, found, err := repo.CountdownClaim(ctx)
			if err != nil {
				return modulesrpc.CountdownDeliveryReply{Refusal: s.refusal("countdown.claim", err)}
			}
			if !found {
				return modulesrpc.CountdownDeliveryReply{}
			}
			return modulesrpc.CountdownDeliveryReply{Found: true, Delivery: modulesrpc.CountdownDelivery{UserID: strconv.FormatUint(d.UserID, 10), Name: d.Name, Generation: d.Generation, Token: d.Token, ExpiresAtMS: d.ExpiresAtMS, Progress: d.Progress}}
		}),
		bus.At("ack", func(ctx context.Context, req modulesrpc.CountdownDeliveryRequest) modulesrpc.CountdownDeliveryReply {
			d := req.Delivery
			id, err := strconv.ParseUint(d.UserID, 10, 64)
			if err != nil {
				return modulesrpc.CountdownDeliveryReply{Refusal: s.refusal("countdown.ack", repository.ErrInvalidInput)}
			}
			found, err := repo.CountdownAck(ctx, repository.CountdownDelivery{UserID: id, Name: d.Name, Generation: d.Generation, Token: d.Token, ExpiresAtMS: d.ExpiresAtMS})
			if err != nil {
				return modulesrpc.CountdownDeliveryReply{Refusal: s.refusal("countdown.ack", err)}
			}
			return modulesrpc.CountdownDeliveryReply{Found: found}
		}),
	); err != nil {
		return err
	}
	return bus.Serve(w, prefix+".countdown.progress", func(ctx context.Context, req modulesrpc.CountdownProgressRequest) modulesrpc.CountdownDeliveryReply {
		d := req.Delivery
		id, err := strconv.ParseUint(d.UserID, 10, 64)
		if err != nil {
			return modulesrpc.CountdownDeliveryReply{Refusal: s.refusal("countdown.progress", repository.ErrInvalidInput)}
		}
		found, err := repo.CountdownProgress(ctx, repository.CountdownDelivery{UserID: id, Name: d.Name, Generation: d.Generation, Token: d.Token, ExpiresAtMS: d.ExpiresAtMS}, req.Key, req.Status)
		if err != nil {
			return modulesrpc.CountdownDeliveryReply{Refusal: s.refusal("countdown.progress", err)}
		}
		return modulesrpc.CountdownDeliveryReply{Found: found}
	})
}
