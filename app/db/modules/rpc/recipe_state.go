// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/modules/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"
)

type recipeStateRPC struct {
	repo *repository.RecipeState
	log  *zap.Logger
}

// SubscribeRecipeState serves prefix.variable.* and prefix.cooldown.*.
func SubscribeRecipeState(w bus.RPCWiring, repo *repository.RecipeState, prefix string) error {
	s := &recipeStateRPC{repo: repo, log: w.Log}
	err := bus.ServeVerbs(w, prefix+".variable",
		variableVerb("get", s.handleVariableGet),
		variableVerb("set", s.handleVariableSet),
		variableVerb("add", s.handleVariableAdd),
		variableVerb("delete", s.handleVariableDelete),
	)
	if err != nil {
		return err
	}
	return bus.ServeVerbs(w, prefix+".cooldown",
		cooldownVerb("claim", s.handleCooldownClaim),
		cooldownVerb("release", s.handleCooldownRelease),
	)
}

func variableVerb(name string, load func(context.Context, modulesrpc.VariableRequest, uint64) (modulesrpc.VariableReply, error)) bus.Verb[modulesrpc.VariableRequest, modulesrpc.VariableReply] {
	return bus.VerbForUser[modulesrpc.VariableRequest, modulesrpc.VariableReply](name, load)
}

func cooldownVerb(name string, load func(context.Context, modulesrpc.CooldownRequest, uint64) (modulesrpc.CooldownReply, error)) bus.Verb[modulesrpc.CooldownRequest, modulesrpc.CooldownReply] {
	return bus.VerbForUser[modulesrpc.CooldownRequest, modulesrpc.CooldownReply](name, load)
}

func (s *recipeStateRPC) refusal(op string, err error) domainrpc.Refusal {
	if errors.Is(err, repository.ErrInvalidInput) {
		return domainrpc.Refused(domainrpc.CodeInvalid, err.Error())
	}
	s.log.Warn("modules "+op+" failed", zap.Error(err))
	return domainrpc.Refused(domainrpc.CodeInternal, "modules request failed")
}

func variableOwner(req modulesrpc.VariableRequest, userID uint64) (repository.VariableOwner, error) {
	owner := repository.VariableOwner{UserID: userID, Scope: req.Scope}
	if req.ViewerID == "" {
		return owner, nil
	}
	viewerID, err := strconv.ParseUint(req.ViewerID, 10, 64)
	if err != nil {
		return owner, errors.Join(repository.ErrInvalidInput, errors.New("viewer_id"))
	}
	owner.ViewerID = viewerID
	return owner, nil
}

func (s *recipeStateRPC) handleVariableGet(ctx context.Context, req modulesrpc.VariableRequest, userID uint64) (modulesrpc.VariableReply, error) {
	owner, err := variableOwner(req, userID)
	if err != nil {
		return s.variableFailure("variable.get", err), nil
	}
	values, err := s.repo.VariablesGet(ctx, owner, req.Names)
	if err != nil {
		return s.variableFailure("variable.get", err), nil
	}
	return modulesrpc.VariableReply{Found: len(values) > 0, Variables: values}, nil
}

func (s *recipeStateRPC) handleVariableSet(ctx context.Context, req modulesrpc.VariableRequest, userID uint64) (modulesrpc.VariableReply, error) {
	return s.variableWrite(req, userID, "variable.set", func(owner repository.VariableOwner) error {
		return s.repo.VariableSet(ctx, owner, req.Name, req.Text)
	}), nil
}

func (s *recipeStateRPC) handleVariableAdd(ctx context.Context, req modulesrpc.VariableRequest, userID uint64) (modulesrpc.VariableReply, error) {
	return s.variableWrite(req, userID, "variable.add", func(owner repository.VariableOwner) error {
		return s.repo.VariableAdd(ctx, owner, req.Name, req.Value)
	}), nil
}

func (s *recipeStateRPC) handleVariableDelete(ctx context.Context, req modulesrpc.VariableRequest, userID uint64) (modulesrpc.VariableReply, error) {
	return s.variableWrite(req, userID, "variable.delete", func(owner repository.VariableOwner) error {
		return s.repo.VariableDelete(ctx, owner, req.Name)
	}), nil
}

func (s *recipeStateRPC) variableWrite(req modulesrpc.VariableRequest, userID uint64, op string, write func(repository.VariableOwner) error) modulesrpc.VariableReply {
	owner, err := variableOwner(req, userID)
	if err == nil {
		err = write(owner)
	}
	if err != nil {
		return s.variableFailure(op, err)
	}
	return modulesrpc.VariableReply{Found: true}
}

func (s *recipeStateRPC) variableFailure(op string, err error) modulesrpc.VariableReply {
	if errors.Is(err, repository.ErrVariableLimit) {
		return modulesrpc.VariableReply{LimitExceeded: true}
	}
	return modulesrpc.VariableReply{Refusal: s.refusal(op, err)}
}

func (s *recipeStateRPC) handleCooldownClaim(ctx context.Context, req modulesrpc.CooldownRequest, userID uint64) (modulesrpc.CooldownReply, error) {
	claim := repository.CooldownClaim{UserID: userID, Windows: make([]repository.CooldownWindow, len(req.Windows))}
	for i, window := range req.Windows {
		claim.Windows[i] = repository.CooldownWindow{Key: window.Key, TTL: time.Duration(window.TTLSeconds) * time.Second}
	}
	holds, won, err := s.repo.CooldownClaimAll(ctx, claim)
	if err != nil {
		return modulesrpc.CooldownReply{Refusal: s.refusal("cooldown.claim", err)}, nil
	}
	views := make([]modulesrpc.CooldownWindow, len(holds))
	for i, hold := range holds {
		views[i] = modulesrpc.CooldownWindow{Key: hold.Key, ExpiresAtMS: hold.ExpiresAtMS}
	}
	return modulesrpc.CooldownReply{Claimed: won, Windows: views}, nil
}

func (s *recipeStateRPC) handleCooldownRelease(ctx context.Context, req modulesrpc.CooldownRequest, userID uint64) (modulesrpc.CooldownReply, error) {
	holds := make([]repository.CooldownHold, len(req.Windows))
	for i, window := range req.Windows {
		holds[i] = repository.CooldownHold{Key: window.Key, ExpiresAtMS: window.ExpiresAtMS}
	}
	if err := s.repo.CooldownRelease(ctx, userID, holds); err != nil {
		return modulesrpc.CooldownReply{Refusal: s.refusal("cooldown.release", err)}, nil
	}
	return modulesrpc.CooldownReply{Found: true}, nil
}
