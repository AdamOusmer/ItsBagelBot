// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"ItsBagelBot/app/deployer/internal/ports"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/deploy"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
)

type verdict struct {
	Actor     deploy.Actor
	Forbidden bool
	Asked     usersrpc.AuthRequest
}

func decide(reply usersrpc.AuthReply, transport error) verdict {
	var asked usersrpc.AuthRequest
	a := &Authorizer{ask: func(_ context.Context, req usersrpc.AuthRequest) (usersrpc.AuthReply, error) {
		asked = req
		return reply, transport
	}}
	actor, err := a.RequireOwner(context.Background(), ownerID)
	return verdict{Actor: actor, Forbidden: errors.Is(err, ports.ErrForbidden), Asked: asked}
}

func TestRequireOwner(t *testing.T) {
	asked := usersrpc.AuthRequest{UserID: ownerID}
	cases := []struct {
		name      string
		reply     usersrpc.AuthReply
		transport error
		want      verdict
	}{
		{"active owner", usersrpc.AuthReply{Admin: true, Role: "owner", Login: ownerLogin}, nil,
			verdict{Actor: deploy.Actor{ID: ownerID, Login: ownerLogin}, Asked: asked}},
		{"admin is not owner", usersrpc.AuthReply{Admin: true, Role: "admin", Login: ownerLogin}, nil,
			verdict{Forbidden: true, Asked: asked}},
		{"moderator is not owner", usersrpc.AuthReply{Admin: true, Role: "moderator"}, nil,
			verdict{Forbidden: true, Asked: asked}},
		{"deactivated owner row", usersrpc.AuthReply{Admin: false, Role: "owner"}, nil,
			verdict{Forbidden: true, Asked: asked}},
		{"users refusal", usersrpc.AuthReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "bad id")}, nil,
			verdict{Forbidden: true, Asked: asked}},
		{"users unreachable", usersrpc.AuthReply{}, context.DeadlineExceeded,
			verdict{Forbidden: true, Asked: asked}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, decide(tc.reply, tc.transport))
		})
	}
}
