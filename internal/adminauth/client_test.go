// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package adminauth_test

import (
	"context"
	"testing"

	"ItsBagelBot/internal/adminauth"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAuthorizeUsesAdminServiceRole(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reply   usersrpc.AuthReply
		wantErr bool
	}{
		{"owner", usersrpc.AuthReply{Admin: true, Role: "owner"}, false},
		{"admin", usersrpc.AuthReply{Admin: true, Role: "admin"}, false},
		{"moderator", usersrpc.AuthReply{Admin: true, Role: "moderator"}, true},
		{"inactive", usersrpc.AuthReply{Role: "owner"}, true},
		{"unknown role", usersrpc.AuthReply{Admin: true, Role: "staff"}, true},
		{"store failed", usersrpc.AuthReply{Refusal: domainrpc.Refused(domainrpc.CodeUnavailable, "store unavailable")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nc := testnats.Connect(t)
			received := make(chan usersrpc.AuthRequest, 1)
			require.NoError(t, bus.Serve(bus.RPCWiring{NC: nc, Log: zap.NewNop()}, adminauth.DefaultAuthPrefix+".check", func(_ context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply { received <- req; return tc.reply }))
			_, err := adminauth.New(nc, adminauth.Config{}).Authorize(t.Context(), "42", adminauth.Admin)
			require.Equal(t, tc.wantErr, err != nil)
			require.Equal(t, usersrpc.AuthRequest{UserID: "42"}, <-received)
		})
	}
}

func TestAuthorizeFailsClosedWithoutAdminService(t *testing.T) {
	client := adminauth.New(testnats.Connect(t), adminauth.Config{})
	_, err := client.Authorize(t.Context(), "42", adminauth.Moderator)
	require.Error(t, err)
}
