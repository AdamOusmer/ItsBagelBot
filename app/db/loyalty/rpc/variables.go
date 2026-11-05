// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

func variableOwner(req loyaltyrpc.Request) (repository.VariableOwner, bool, loyaltyrpc.Reply) {
	userID, viewerID, ok, reply := parseIDs(req, false)
	return repository.VariableOwner{UserID: userID, ViewerID: viewerID, Scope: req.Scope}, ok, reply
}

func (l *loyaltyRPC) handleVariableGet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	owner, ok, reply := variableOwner(req)
	if !ok {
		return reply
	}
	values, err := l.repo.VariablesGet(ctx, owner, req.Names)
	if err != nil {
		return l.variableFailure("loyalty variable.get", err)
	}
	return loyaltyrpc.Reply{Found: len(values) > 0, Variables: values}
}

func (l *loyaltyRPC) handleVariableSet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	return l.variableWrite(ctx, req, "loyalty variable.set", func(ctx context.Context, owner repository.VariableOwner) error {
		return l.repo.VariableSet(ctx, owner, req.Name, req.Text)
	})
}

func (l *loyaltyRPC) handleVariableAdd(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	return l.variableWrite(ctx, req, "loyalty variable.add", func(ctx context.Context, owner repository.VariableOwner) error {
		return l.repo.VariableAdd(ctx, owner, req.Name, req.Value)
	})
}

func (l *loyaltyRPC) handleVariableDelete(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	return l.variableWrite(ctx, req, "loyalty variable.delete", func(ctx context.Context, owner repository.VariableOwner) error {
		return l.repo.VariableDelete(ctx, owner, req.Name)
	})
}

func (l *loyaltyRPC) variableWrite(ctx context.Context, req loyaltyrpc.Request, op string, write func(context.Context, repository.VariableOwner) error) loyaltyrpc.Reply {
	owner, ok, reply := variableOwner(req)
	if !ok {
		return reply
	}
	if err := write(ctx, owner); err != nil {
		return l.variableFailure(op, err)
	}
	return loyaltyrpc.Reply{Found: true}
}

func (l *loyaltyRPC) variableFailure(op string, err error) loyaltyrpc.Reply {
	if errors.Is(err, repository.ErrVariableLimit) {
		return loyaltyrpc.Reply{LimitExceeded: true}
	}
	return l.fail(op, err)
}
