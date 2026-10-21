// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"

	"ItsBagelBot/app/db/users/ent/adminuser"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveStaffKeepsTheLastActiveOwner(t *testing.T) {
	h := newHarness(t)
	h.staff(
		staffFixture{101, adminuser.RoleOwner, true},
		staffFixture{103, adminuser.RoleOwner, false},
	)

	reply := h.auth(t, "remove", usersrpc.AuthRequest{ActorID: "101", UserID: "103"})

	assert.Equal(t, "cannot remove the last owner", reply.Error)
	assert.True(t, h.client.AdminUser.GetX(t.Context(), 101).Active)
}

func TestRemoveStaffRefusesSelfAndMissingTarget(t *testing.T) {
	h := newHarness(t)
	h.staff(staffFixture{101, adminuser.RoleOwner, true})

	assert.Equal(t, "forbidden: cannot remove yourself", h.auth(t, "remove", usersrpc.AuthRequest{ActorID: "101", UserID: "101"}).Error)
	assert.Equal(t, "staff not found", h.auth(t, "remove", usersrpc.AuthRequest{ActorID: "101", UserID: absentTarget}).Error)
}

func TestUpsertStaffGuardsExistingTargets(t *testing.T) {
	h := newHarness(t)
	h.staff(
		staffFixture{100, adminuser.RoleAdmin, true},
		staffFixture{101, adminuser.RoleOwner, true},
		staffFixture{110, adminuser.RoleAdmin, true},
		staffFixture{200, adminuser.RoleModerator, false},
	)

	assert.Equal(t, "forbidden: an owner's role cannot be changed",
		h.auth(t, "upsert", usersrpc.AuthRequest{ActorID: "100", UserID: "101", Login: "x", Role: "admin"}).Error)
	assert.Equal(t, "forbidden: admins cannot change another admin",
		h.auth(t, "upsert", usersrpc.AuthRequest{ActorID: "100", UserID: "110", Login: "x", Role: "moderator"}).Error)

	reply := h.auth(t, "upsert", usersrpc.AuthRequest{ActorID: "100", UserID: "200", Login: "back", Role: "moderator"})
	require.Empty(t, reply.Error)
	stored := h.client.AdminUser.GetX(t.Context(), 200)
	assert.True(t, stored.Active)
	assert.Equal(t, "back", stored.Login)
}
