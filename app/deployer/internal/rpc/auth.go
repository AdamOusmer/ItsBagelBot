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

// ownerRole is adminuser.RoleOwner as auth.check puts it on the wire. Spelled
// here because the deployer must not import the users service's ent schema
// for one constant; app/db/transactions compares the same literal.
const ownerRole = "owner"

// authTimeout bounds the auth.check hop inside the verb's own budget.
// auth.check is one indexed staff-row read and the transactions service gives
// it the same 5s (giveaways_users.go). Without it a wedged users service would
// hold every deploy verb for the whole 20s handler budget before refusing.
const authTimeout = 5 * time.Second

// Authorizer implements ports.Authorizer against the users service.
type Authorizer struct {
	ask func(context.Context, usersrpc.AuthRequest) (usersrpc.AuthReply, error)
}

var _ ports.Authorizer = (*Authorizer)(nil)

// AuthSubject is the users service staff check the Authorizer asks.
type AuthSubject string

// NewAuthorizer asks subject (bagel.rpc.admin.user.auth.check) over nc, the
// same verb the console's requireAdmin resolves a session through.
func NewAuthorizer(nc *nats.Conn, subject AuthSubject) *Authorizer {
	return &Authorizer{ask: func(ctx context.Context, req usersrpc.AuthRequest) (usersrpc.AuthReply, error) {
		return bus.RequestJSONTimeout[usersrpc.AuthReply](ctx, nc, string(subject), req, authTimeout)
	}}
}

// RequireOwner admits only a caller the users service positively names as
// active staff with the owner role, read from its staff table. It fails
// closed: a transport error, a timeout, an error reply and a non-owner all
// come back as ErrForbidden. A users outage therefore reads as forbidden
// rather than unavailable; that is the price of there being no path out of
// here that admits an unverified caller.
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

// isOwner requires Admin as well as the role: auth.check answers admin:false
// for an unknown or deactivated staff row, and a role on such a reply is not
// a grant.
func isOwner(r usersrpc.AuthReply) bool { return r.Admin && r.Role == ownerRole }
