// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package rpc

import (
	"ItsBagelBot/app/db/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"
	"context"
	"errors"
	"strconv"
)

func SubscribeCountdown(w bus.RPCWiring, repo *repository.RecipeState, prefix string) error {
	s := &recipeStateRPC{repo: repo, log: w.Log}
	subject := prefix + ".countdown"
	if err := bus.ServeVerbs(w, subject, s.countdownVerb("start"), s.countdownVerb("add"), s.countdownVerb("peek")); err != nil {
		return err
	}
	if err := bus.ServeVerbs(w, subject, bus.At("claim", s.countdownClaim), bus.At("ack", s.countdownAck)); err != nil {
		return err
	}
	return bus.Serve(w, subject+".progress", s.countdownProgress)
}
func (s *recipeStateRPC) countdownVerb(name string) bus.Verb[modulesrpc.CountdownRequest, modulesrpc.CountdownReply] {
	return bus.VerbForUser[modulesrpc.CountdownRequest, modulesrpc.CountdownReply](name, func(ctx context.Context, req modulesrpc.CountdownRequest, id uint64) (modulesrpc.CountdownReply, error) {
		return s.countdownWrite(ctx, req, id, name), nil
	})
}
func (s *recipeStateRPC) countdownWrite(ctx context.Context, req modulesrpc.CountdownRequest, id uint64, verb string) modulesrpc.CountdownReply {
	result, err := s.countdownResult(ctx, req, id, verb)
	if errors.Is(err, repository.ErrVariableLimit) {
		return modulesrpc.CountdownReply{LimitExceeded: true}
	}
	if err != nil {
		return modulesrpc.CountdownReply{Refusal: s.refusal("countdown."+verb, err)}
	}
	return modulesrpc.CountdownReply{Found: result.Found, Capped: result.Capped, ExpiresAtMS: result.ExpiresAtMS}
}
func (s *recipeStateRPC) countdownResult(ctx context.Context, req modulesrpc.CountdownRequest, id uint64, verb string) (repository.CountdownResult, error) {
	if verb == "peek" {
		return s.repo.CountdownPeek(ctx, id, req.Name)
	}
	return s.repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: id, Name: req.Name, Verb: verb, Seconds: req.Seconds, OperationID: req.OperationID})
}
func (s *recipeStateRPC) countdownClaim(ctx context.Context, _ modulesrpc.CountdownDeliveryRequest) modulesrpc.CountdownDeliveryReply {
	d, found, err := s.repo.CountdownClaim(ctx)
	if err != nil {
		return modulesrpc.CountdownDeliveryReply{Refusal: s.refusal("countdown.claim", err)}
	}
	if !found {
		return modulesrpc.CountdownDeliveryReply{}
	}
	return modulesrpc.CountdownDeliveryReply{Found: true, Delivery: modulesrpc.CountdownDelivery{UserID: strconv.FormatUint(d.UserID, 10), Name: d.Name, Generation: d.Generation, Token: d.Token, ExpiresAtMS: d.ExpiresAtMS, Progress: d.Progress}}
}
func parseCountdownDelivery(d modulesrpc.CountdownDelivery) (repository.CountdownDelivery, error) {
	id, err := strconv.ParseUint(d.UserID, 10, 64)
	if err != nil {
		return repository.CountdownDelivery{}, repository.ErrInvalidInput
	}
	return repository.CountdownDelivery{UserID: id, Name: d.Name, Generation: d.Generation, Token: d.Token, ExpiresAtMS: d.ExpiresAtMS}, nil
}
func (s *recipeStateRPC) deliveryAction(d modulesrpc.CountdownDelivery, op string, run func(repository.CountdownDelivery) (bool, error)) modulesrpc.CountdownDeliveryReply {
	delivery, err := parseCountdownDelivery(d)
	if err == nil {
		var found bool
		found, err = run(delivery)
		if err == nil {
			return modulesrpc.CountdownDeliveryReply{Found: found}
		}
	}
	return modulesrpc.CountdownDeliveryReply{Refusal: s.refusal("countdown."+op, err)}
}
func (s *recipeStateRPC) countdownAck(ctx context.Context, req modulesrpc.CountdownDeliveryRequest) modulesrpc.CountdownDeliveryReply {
	return s.deliveryAction(req.Delivery, "ack", func(d repository.CountdownDelivery) (bool, error) { return s.repo.CountdownAck(ctx, d) })
}
func (s *recipeStateRPC) countdownProgress(ctx context.Context, req modulesrpc.CountdownProgressRequest) modulesrpc.CountdownDeliveryReply {
	return s.deliveryAction(req.Delivery, "progress", func(d repository.CountdownDelivery) (bool, error) {
		return s.repo.CountdownProgress(ctx, repository.CountdownProgressRequest{Delivery: d, Key: req.Key, Status: req.Status})
	})
}
