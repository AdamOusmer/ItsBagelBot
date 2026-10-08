// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent/adminuser"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	modActor    = uint64(7001)
	adminActor  = uint64(7002)
	ownerActor  = uint64(7003)
	inactiveMod = uint64(7004)
	listPage    = repository.AdminUserPageSize
)

type pageMeta struct {
	Page, PageSize, MaxPages int
	HasMore                  bool
}

func metaOf(reply usersrpc.AdminReply) pageMeta {
	return pageMeta{Page: reply.Page, PageSize: reply.PageSize, MaxPages: reply.MaxPages, HasMore: reply.HasMore}
}

func newestFirst(from, to int) []string {
	var names []string
	for i := from; i >= to; i-- {
		names = append(names, fmt.Sprintf("user-%02d", i))
	}
	return names
}

func moderatorHarness(t *testing.T) harness {
	t.Helper()
	h := newHarness(t)
	h.staff(staffFixture{modActor, adminuser.RoleModerator, true})
	return h
}

func TestAdminUserListPagesNewestFirst(t *testing.T) {
	h := moderatorHarness(t)
	h.users(2*listPage, time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC))

	first := h.admin(t, "list", modActor, usersrpc.AdminRequest{Page: 1, Limit: listPage})
	second := h.admin(t, "list", modActor, usersrpc.AdminRequest{Page: 2, Limit: listPage})

	require.Empty(t, first.Error)
	assert.Equal(t, pageMeta{1, listPage, repository.AdminUserMaxPages, true}, metaOf(first))
	assert.Equal(t, newestFirst(2*listPage-1, listPage), usernames(first))
	require.Empty(t, second.Error)
	assert.Equal(t, pageMeta{2, listPage, repository.AdminUserMaxPages, false}, metaOf(second))
	assert.Equal(t, newestFirst(listPage-1, 0), usernames(second))
}

func TestAdminUserListSearchesBeforePaging(t *testing.T) {
	h := moderatorHarness(t)
	base := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	h.users(2*listPage, base)
	h.client.User.Create().SetID(424242).SetUsername("needle-user").SetEmail("needle@example.invalid").
		SetStatus(user.StatusVip).SetUpdatedAt(base.Add(2 * time.Hour)).ExecX(t.Context())

	reply := h.admin(t, "list", modActor, usersrpc.AdminRequest{Page: 1, Limit: listPage, Search: "NEEDLE"})

	require.Empty(t, reply.Error)
	assert.Equal(t, []uint64{424242}, userIDs(reply))
	assert.False(t, reply.HasMore)
}

func TestAdminUserListFiltersByState(t *testing.T) {
	h := moderatorHarness(t)
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	seed := []struct {
		id     uint64
		status user.Status
		active bool
		banned bool
	}{
		{6001, user.StatusVip, true, false},
		{6002, user.StatusPaid, true, false},
		{6003, user.StatusFree, true, false},
		{6004, user.StatusVip, false, false},
		{6005, user.StatusPaid, false, true},
	}
	for i, s := range seed {
		h.client.User.Create().SetID(s.id).
			SetUsername(fmt.Sprintf("state-%02d", i)).SetEmail(fmt.Sprintf("state-%02d@example.invalid", i)).
			SetStatus(s.status).SetIsActive(s.active).SetBanned(s.banned).
			SetUpdatedAt(base.Add(time.Duration(i) * time.Minute)).ExecX(t.Context())
	}

	for _, tc := range []struct {
		state string
		want  []uint64
	}{
		{"vip", []uint64{6001}},
		{"paid", []uint64{6002}},
		{"free", []uint64{6003}},
		{"inactive", []uint64{6004}},
		{"banned", []uint64{6005}},
		{"nope", []uint64{6005, 6004, 6003, 6002, 6001}},
	} {
		t.Run(tc.state, func(t *testing.T) {
			reply := h.admin(t, "list", modActor, usersrpc.AdminRequest{Page: 1, Limit: listPage, State: tc.state})

			require.Empty(t, reply.Error)
			assert.Equal(t, tc.want, userIDs(reply))
		})
	}
}

func TestAdminEnrollmentBucketsPerDay(t *testing.T) {
	h := moderatorHarness(t)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	for i, ts := range []time.Time{day, day.Add(time.Second), day.AddDate(0, 0, -1), day.AddDate(0, 0, -10)} {
		h.client.User.Create().SetID(uint64(7100 + i)).
			SetUsername(fmt.Sprintf("enroll-%02d", i)).SetEmail(fmt.Sprintf("enroll-%02d@example.invalid", i)).
			SetStatus(user.StatusFree).SetCreatedAt(ts).SetUpdatedAt(ts).ExecX(t.Context())
	}

	reply := h.admin(t, "enrollment", modActor, usersrpc.AdminRequest{Days: 7})

	require.Empty(t, reply.Error)
	require.NotNil(t, reply.Enrollment)
	counts := map[string]int{}
	for _, d := range reply.Enrollment.Days {
		counts[d.Date] = d.Count
	}
	assert.Equal(t, map[string]int{
		day.Format(time.DateOnly):                   2,
		day.AddDate(0, 0, -1).Format(time.DateOnly): 1,
		day.AddDate(0, 0, -2).Format(time.DateOnly): 0,
		day.AddDate(0, 0, -3).Format(time.DateOnly): 0,
		day.AddDate(0, 0, -4).Format(time.DateOnly): 0,
		day.AddDate(0, 0, -5).Format(time.DateOnly): 0,
		day.AddDate(0, 0, -6).Format(time.DateOnly): 0,
	}, counts)
	assert.Equal(t, 4, reply.Enrollment.Stats.TotalUsers)
}

func TestAdminEnrollmentWindow(t *testing.T) {
	h := moderatorHarness(t)

	for _, tc := range []struct {
		name string
		days int
		want int
	}{
		{"defaults when unset", 0, 30},
		{"clamps an oversized window", 500, 90},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := h.admin(t, "enrollment", modActor, usersrpc.AdminRequest{Days: tc.days})

			require.Empty(t, reply.Error)
			require.NotNil(t, reply.Enrollment)
			assert.Len(t, reply.Enrollment.Days, tc.want)
		})
	}
}

func TestAdminVerbsRequireTheirMinimumRole(t *testing.T) {
	h := newHarness(t)
	h.staff(
		staffFixture{modActor, adminuser.RoleModerator, true},
		staffFixture{adminActor, adminuser.RoleAdmin, true},
		staffFixture{ownerActor, adminuser.RoleOwner, true},
	)
	actors := []uint64{modActor, adminActor, ownerActor}

	for _, tc := range []struct {
		verb string
		min  int
	}{
		{"get", 0}, {"list", 0}, {"stats", 0}, {"enrollment", 0}, {"overview", 0},
		{"token_status", 0}, {"ban", 0}, {"unban", 0},
		{"set_status", 1}, {"set_active", 1}, {"set_creator_code", 1}, {"test.set", 1},
		{"reset", 1}, {"token_set", 1}, {"token_clear", 1},
		{"delete", 2},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			for rank, actor := range actors {
				reply := h.admin(t, tc.verb, actor, usersrpc.AdminRequest{UserID: absentTarget, Status: "paid"})

				if rank < tc.min {
					assert.Equal(t, domainrpc.CodeForbidden, reply.Code, "actor %d", actor)
					continue
				}
				assert.NotEqual(t, domainrpc.CodeForbidden, reply.Code, "actor %d: %s", actor, reply.Error)
			}
		})
	}
}

func TestAdminGuardRefusesUnidentifiedActors(t *testing.T) {
	h := newHarness(t)
	h.staff(
		staffFixture{modActor, adminuser.RoleModerator, true},
		staffFixture{inactiveMod, adminuser.RoleModerator, false},
	)

	for _, tc := range []struct {
		name  string
		actor string
		want  domainrpc.Code
	}{
		{"a staff moderator may read", fmt.Sprint(modActor), domainrpc.CodeNotFound},
		{"a missing actor is refused", "", domainrpc.CodeForbidden},
		{"an unknown actor is refused", "424242", domainrpc.CodeForbidden},
		{"an inactive actor is refused", fmt.Sprint(inactiveMod), domainrpc.CodeForbidden},
		{"a non-numeric actor is refused", "not-an-id", domainrpc.CodeForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := call[usersrpc.AdminReply](t, h, adminPrefix+".get", usersrpc.AdminRequest{ActorID: tc.actor, UserID: absentTarget})

			assert.Equal(t, tc.want, reply.Code)
			assert.Nil(t, reply.User)
		})
	}
}

func TestInternalGetAnswersWithoutActor(t *testing.T) {
	h := newHarness(t)
	req := usersrpc.AdminRequest{UserID: absentTarget}

	guarded := call[usersrpc.AdminReply](t, h, adminPrefix+".get", req)
	internal := call[usersrpc.AdminReply](t, h, internalGet, req)

	assert.Equal(t, domainrpc.CodeForbidden, guarded.Code)
	assert.Equal(t, domainrpc.CodeNotFound, internal.Code)
	assert.Nil(t, internal.User)
}
