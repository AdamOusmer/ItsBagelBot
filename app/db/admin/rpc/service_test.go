// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"slices"
	"testing"
	"time"

	"ItsBagelBot/app/db/admin/ent/adminuser"
	"ItsBagelBot/app/db/admin/ent/analyticsrollup"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"github.com/stretchr/testify/require"
)

func TestCheckUsesPersistedStaffAndRefreshesIdentity(t *testing.T) {
	h := newHarness(t)
	h.staff(staffFixture{101, adminuser.RoleAdmin, true}, staffFixture{102, adminuser.RoleOwner, false})
	for _, tc := range []struct {
		id    string
		admin bool
		role  string
	}{
		{"101", true, "admin"},
		{"102", false, ""},
		{"999", false, ""},
	} {
		reply := h.auth(t, "check", usersrpc.AuthRequest{UserID: tc.id, ActorRole: "owner", Login: "renamed", DisplayName: "New name"})
		require.Empty(t, reply.Error)
		require.Equal(t, tc.admin, reply.Admin)
		require.Equal(t, tc.role, reply.Role)
	}

	stored := h.client.AdminUser.GetX(t.Context(), 101)
	require.Equal(t, "renamed", stored.Login)
	require.Equal(t, "New name", stored.DisplayName)
	require.Equal(t, adminuser.RoleAdmin, stored.Role)
}

func TestActiveStaffReturnsOnlyPersistedActiveMembers(t *testing.T) {
	h := newHarness(t)
	h.staff(staffFixture{101, adminuser.RoleAdmin, true}, staffFixture{102, adminuser.RoleOwner, false}, staffFixture{103, adminuser.RoleModerator, true})
	reply := call[usersrpc.AuthReply](t, h, activeSubject, usersrpc.AuthRequest{})
	require.Empty(t, reply.Error)

	ids := make([]uint64, 0, len(reply.Admins))
	for _, row := range reply.Admins {
		ids = append(ids, row.ID)
	}
	slices.Sort(ids)
	require.Equal(t, []uint64{101, 103}, ids)
}

func TestAnalyticsAuthorizesStaffAndReadsAdminDatabase(t *testing.T) {
	h := newHarness(t)
	h.staff(staffFixture{99, adminuser.RoleModerator, true}, staffFixture{100, adminuser.RoleAdmin, true}, staffFixture{101, adminuser.RoleOwner, false})
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	h.client.AnalyticsRollup.Create().SetWindow(analyticsrollup.WindowD).SetPeriodStart(yesterday).
		SetMetric("chat.messages").SetValue(25).ExecX(t.Context())
	for _, tc := range []struct {
		name   string
		actor  string
		metric string
		code   domainrpc.Code
		rows   int
	}{
		{"moderator refused", "99", "chat.messages", domainrpc.CodeForbidden, 0},
		{"inactive owner refused", "101", "chat.messages", domainrpc.CodeForbidden, 0},
		{"missing actor refused", "", "chat.messages", domainrpc.CodeForbidden, 0},
		{"unknown metric refused", "100", "raw.identifiers", domainrpc.CodeInvalid, 0},
		{"admin reads aggregates", "100", "chat.messages", domainrpc.CodeOK, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := call[usersrpc.AdminReply](t, h, analyticsSubject, usersrpc.AdminRequest{
				ActorID: tc.actor, Metrics: []string{tc.metric}, Days: 1, Window: "d",
			})
			require.Equal(t, tc.code, reply.Code)
			if tc.rows == 0 {
				require.Nil(t, reply.Analytics)
				return
			}
			require.Len(t, reply.Analytics.Rows, tc.rows)
			require.Equal(t, int64(25), reply.Analytics.Rows[0].Value)
		})
	}
}
