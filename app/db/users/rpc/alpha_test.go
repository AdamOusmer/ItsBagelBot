// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/users/rpc"
	"ItsBagelBot/internal/adminauth"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAdminAlphaSetGatesAlphaCheck(t *testing.T) {
	h := newHarness(t)
	h.users(1, time.Now())
	h.staff(staffFixture{modActor, adminauth.Moderator, true}, staffFixture{adminActor, adminauth.Admin, true})
	require.NoError(t, rpc.SubscribeAlpha(rpc.Wiring{RPCWiring: bus.RPCWiring{NC: h.nc, Log: zap.NewNop()}, Repo: h.repo}, usersrpc.AlphaCheckSubject))
	check := func() usersrpc.AlphaCheckReply {
		return call[usersrpc.AlphaCheckReply](t, h, usersrpc.AlphaCheckSubject, usersrpc.AlphaCheckRequest{UserID: "9000"})
	}
	assert.False(t, check().Alpha)

	denied := h.admin(t, "alpha.set", modActor, usersrpc.AdminRequest{UserID: "9000", Alpha: true})
	assert.Equal(t, domainrpc.CodeForbidden, denied.Code)
	assert.False(t, check().Alpha)

	granted := h.admin(t, "alpha.set", adminActor, usersrpc.AdminRequest{UserID: "9000", Alpha: true})
	require.Empty(t, granted.Error)
	assert.True(t, granted.User.Alpha)
	assert.True(t, check().Alpha)

	h.admin(t, "alpha.set", adminActor, usersrpc.AdminRequest{UserID: "9000"})
	assert.False(t, check().Alpha)

	missing := call[usersrpc.AlphaCheckReply](t, h, usersrpc.AlphaCheckSubject, usersrpc.AlphaCheckRequest{UserID: "424242"})
	assert.Equal(t, domainrpc.CodeNotFound, missing.Code)
}
