// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"time"

	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const recipeStateRPCTimeout = 3 * time.Second

var ErrVariableLimit = errors.New("variable limit reached")

type VariableOwner struct {
	BroadcasterID uint64
	ViewerID      uint64
	Scope         string
}

type VariableStore interface {
	VariablesGet(ctx context.Context, owner VariableOwner, names []string) (map[string]string, error)
	VariableSet(ctx context.Context, owner VariableOwner, name, value string) error
	VariableAdd(ctx context.Context, owner VariableOwner, name string, amount int64) error
	VariableDelete(ctx context.Context, owner VariableOwner, name string) error
}

// RecipeStateRPC reaches recipe variables and long cooldown windows in the modules service.
type RecipeStateRPC struct {
	nc     *nats.Conn
	prefix string
}

func NewRecipeStateRPC(nc *nats.Conn, modulesPrefix string) *RecipeStateRPC {
	return &RecipeStateRPC{nc: nc, prefix: modulesPrefix}
}

func recipeStateCall[Reply any](ctx context.Context, c *RecipeStateRPC, verb string, req any) (Reply, error) {
	return bus.RequestJSONTimeout[Reply](ctx, c.nc, c.prefix+"."+verb, req, recipeStateRPCTimeout)
}

func variableRequest(owner VariableOwner, name string) modulesrpc.VariableRequest {
	req := modulesrpc.VariableRequest{UserID: fmtID(owner.BroadcasterID), Scope: owner.Scope, Name: name}
	if owner.ViewerID != 0 {
		req.ViewerID = fmtID(owner.ViewerID)
	}
	return req
}

func (c *RecipeStateRPC) variableCall(ctx context.Context, verb string, req modulesrpc.VariableRequest) (modulesrpc.VariableReply, error) {
	reply, err := recipeStateCall[modulesrpc.VariableReply](ctx, c, "variable."+verb, req)
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	return reply, nil
}

func (c *RecipeStateRPC) VariablesGet(ctx context.Context, owner VariableOwner, names []string) (map[string]string, error) {
	req := variableRequest(owner, "")
	req.Names = names
	reply, err := c.variableCall(ctx, "get", req)
	if err != nil {
		return nil, err
	}
	return reply.Variables, nil
}

func (c *RecipeStateRPC) VariableSet(ctx context.Context, owner VariableOwner, name, value string) error {
	req := variableRequest(owner, name)
	req.Text = value
	return c.variableWrite(ctx, "set", req)
}

func (c *RecipeStateRPC) VariableAdd(ctx context.Context, owner VariableOwner, name string, amount int64) error {
	req := variableRequest(owner, name)
	req.Value = amount
	return c.variableWrite(ctx, "add", req)
}

func (c *RecipeStateRPC) VariableDelete(ctx context.Context, owner VariableOwner, name string) error {
	return c.variableWrite(ctx, "delete", variableRequest(owner, name))
}

func (c *RecipeStateRPC) variableWrite(ctx context.Context, verb string, req modulesrpc.VariableRequest) error {
	reply, err := c.variableCall(ctx, verb, req)
	if err != nil {
		return err
	}
	if reply.LimitExceeded {
		return ErrVariableLimit
	}
	return nil
}
