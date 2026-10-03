// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/adminuser"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const auditEntries = 30

func (h harness) auditEntries(n int, base time.Time) {
	for i := range n {
		h.client.AdminAudit.Create().
			SetActorID(uint64(1000 + i%2)).
			SetActorLogin(fmt.Sprintf("actor-%02d", i)).
			SetAction("set_status").
			SetTarget(fmt.Sprintf("user-%02d", i)).
			SetDetail(fmt.Sprintf("detail-%02d", i)).
			SetOk(true).
			SetCreatedAt(base.Add(time.Duration(i) * time.Minute)).
			ExecX(context.Background())
	}
}

func targets(reply usersrpc.AuthReply) []string {
	names := make([]string, 0, len(reply.Entries))
	for _, e := range reply.Entries {
		names = append(names, e.Target)
	}
	return names
}

func TestAuditListPagesNewestFirst(t *testing.T) {
	h := newHarness(t)
	h.auditEntries(auditEntries, time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC))
	const pageSize = 10

	for _, tc := range []struct {
		page    int
		first   int
		hasMore bool
	}{
		{1, 29, true},
		{2, 19, true},
		{3, 9, false},
	} {
		t.Run(fmt.Sprintf("page %d", tc.page), func(t *testing.T) {
			reply := h.audit(t, "list", usersrpc.AuthRequest{Page: tc.page, Limit: pageSize})

			require.Empty(t, reply.Error)
			want := make([]string, 0, pageSize)
			for i := tc.first; i > tc.first-pageSize; i-- {
				want = append(want, fmt.Sprintf("user-%02d", i))
			}
			assert.Equal(t, want, targets(reply))
			assert.Equal(t, tc.page, reply.Page)
			assert.Equal(t, pageSize, reply.PageSize)
			assert.Equal(t, tc.hasMore, reply.HasMore)
		})
	}

	beyond := h.audit(t, "list", usersrpc.AuthRequest{Page: 10_000, Limit: pageSize})
	assert.Equal(t, beyond.MaxPages, beyond.Page, "a page past the cap is clamped to the last allowed page")
}

func TestAuditListSearchesBeforePaging(t *testing.T) {
	h := newHarness(t)
	base := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	h.auditEntries(auditEntries, base)
	h.client.AdminAudit.Create().
		SetActorID(4242).
		SetActorLogin("itsmavey").
		SetAction("staff_upsert").
		SetTarget("needle-target").
		SetDetail("Promoted through the audit search needle").
		SetOk(true).
		SetCreatedAt(base.Add(2 * time.Hour)).
		ExecX(t.Context())

	reply := h.audit(t, "list", usersrpc.AuthRequest{Page: 1, Limit: 10, Search: "NEEDLE"})

	require.Empty(t, reply.Error)
	require.Len(t, reply.Entries, 1)
	assert.Equal(t, "itsmavey", reply.Entries[0].ActorLogin)
	assert.Equal(t, "staff_upsert", reply.Entries[0].Action)
	assert.False(t, reply.HasMore)
}

func TestUpsertStaffAuthorizesStoredActorRole(t *testing.T) {
	h := newHarness(t)
	h.staff(
		staffFixture{99, adminuser.RoleModerator, true},
		staffFixture{100, adminuser.RoleAdmin, true},
		staffFixture{101, adminuser.RoleOwner, true},
		staffFixture{102, adminuser.RoleOwner, false},
	)

	for _, tc := range []struct {
		name      string
		actor     string
		claimed   string
		target    string
		role      string
		wantError string
		stored    bool
	}{
		{"a moderator cannot spoof owner", "99", "owner", "199", "moderator", "forbidden: managers only", false},
		{"an admin cannot grant owner by claiming it", "100", "owner", "200", "owner", "forbidden: only an owner can grant owner", false},
		{"an inactive owner is refused", "102", "owner", "201", "moderator", "forbidden: actor is not active staff", false},
		{"an owner may grant owner whatever role it claims", "101", "moderator", "200", "owner", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := h.auth(t, "upsert", usersrpc.AuthRequest{
				ActorID: tc.actor, ActorRole: tc.claimed, UserID: tc.target, Login: "new-staff-" + tc.target, Role: tc.role,
			})

			assert.Equal(t, tc.wantError, reply.Error)
			stored, err := h.client.AdminUser.Get(t.Context(), mustID(t, tc.target))
			if !tc.stored {
				assert.True(t, ent.IsNotFound(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, adminuser.RoleOwner, stored.Role)
			assert.Equal(t, uint64(101), stored.AddedBy)
		})
	}
}

func TestRemoveStaffAuthorizesStoredActorRole(t *testing.T) {
	h := newHarness(t)
	h.staff(
		staffFixture{100, adminuser.RoleAdmin, true},
		staffFixture{101, adminuser.RoleOwner, true},
		staffFixture{102, adminuser.RoleOwner, true},
		staffFixture{103, adminuser.RoleOwner, false},
		staffFixture{200, adminuser.RoleModerator, true},
	)

	for _, tc := range []struct {
		name       string
		actor      string
		claimed    string
		target     uint64
		wantError  string
		wantActive bool
	}{
		{"an admin cannot remove an owner by claiming owner", "100", "owner", 102, "forbidden: cannot remove an owner", true},
		{"an inactive owner is refused", "103", "owner", 200, "forbidden: actor is not active staff", true},
		{"an owner may remove another owner whatever role it claims", "101", "moderator", 102, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := h.auth(t, "remove", usersrpc.AuthRequest{ActorID: tc.actor, ActorRole: tc.claimed, UserID: fmt.Sprint(tc.target)})

			assert.Equal(t, tc.wantError, reply.Error)
			assert.Equal(t, tc.wantActive, h.client.AdminUser.GetX(t.Context(), tc.target).Active)
		})
	}
}

func mustID(t *testing.T, raw string) uint64 {
	t.Helper()
	var id uint64
	_, err := fmt.Sscan(raw, &id)
	require.NoError(t, err)
	return id
}
