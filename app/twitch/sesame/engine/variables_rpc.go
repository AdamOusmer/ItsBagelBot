// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

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

func variableRequest(owner VariableOwner, name string) loyaltyrpc.Request {
	req := loyaltyrpc.Request{UserID: fmtID(owner.BroadcasterID), Scope: owner.Scope, Name: name}
	if owner.ViewerID != 0 {
		req.ViewerID = fmtID(owner.ViewerID)
	}
	return req
}

func (l *LoyaltyRPC) VariablesGet(ctx context.Context, owner VariableOwner, names []string) (map[string]string, error) {
	req := variableRequest(owner, "")
	req.Names = names
	reply, err := l.call(ctx, "variable.get", req)
	if err != nil {
		return nil, err
	}
	return reply.Variables, nil
}

func (l *LoyaltyRPC) VariableSet(ctx context.Context, owner VariableOwner, name, value string) error {
	req := variableRequest(owner, name)
	req.Text = value
	return l.variableWrite(ctx, "variable.set", req)
}

func (l *LoyaltyRPC) VariableAdd(ctx context.Context, owner VariableOwner, name string, amount int64) error {
	req := variableRequest(owner, name)
	req.Value = amount
	return l.variableWrite(ctx, "variable.add", req)
}

func (l *LoyaltyRPC) VariableDelete(ctx context.Context, owner VariableOwner, name string) error {
	return l.variableWrite(ctx, "variable.delete", variableRequest(owner, name))
}

func (l *LoyaltyRPC) variableWrite(ctx context.Context, verb loyaltyVerb, req loyaltyrpc.Request) error {
	reply, err := l.call(ctx, verb, req)
	if err != nil {
		return err
	}
	if reply.LimitExceeded {
		return ErrVariableLimit
	}
	return nil
}
