// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

const ownerRole = "owner"

const authTimeout = 5 * time.Second

type Authorizer struct {
	ask func(context.Context, usersrpc.AuthRequest) (usersrpc.AuthReply, error)
}

var _ ports.Authorizer = (*Authorizer)(nil)

type AuthSubject string

func NewAuthorizer(nc *nats.Conn, subject AuthSubject) *Authorizer {
	return &Authorizer{ask: func(ctx context.Context, req usersrpc.AuthRequest) (usersrpc.AuthReply, error) {
		return bus.RequestJSONTimeout[usersrpc.AuthReply](ctx, nc, string(subject), req, authTimeout)
	}}
}

func (a *Authorizer) RequireOwner(ctx context.Context, actorID string) (deploy.Actor, error) {
	reply, err := a.ask(ctx, usersrpc.AuthRequest{UserID: actorID})
	if err != nil {
		return deploy.Actor{}, fmt.Errorf("%w: resolve actor: %v", ports.ErrForbidden, err)
	}
	if !isOwner(reply) {
		return deploy.Actor{}, ports.ErrForbidden
	}
	return deploy.Actor{ID: actorID, Login: reply.Login}, nil
}

// Admin is required too: a deactivated row keeps its role.
func isOwner(r usersrpc.AuthReply) bool { return r.Admin && r.Role == ownerRole }
