// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/adminauth"
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
	Page, PageSize, MaxPages, Total int
	HasMore                         bool
}

func metaOf(reply usersrpc.AdminReply) pageMeta {
	return pageMeta{Page: reply.Page, PageSize: reply.PageSize, MaxPages: reply.MaxPages, Total: reply.Total, HasMore: reply.HasMore}
}

func seededUsernames(first, count, step int) []string {
	names := make([]string, count)
	for i := range names {
		names[i] = fmt.Sprintf("user-%02d", first+i*step)
	}
	return names
}

func moderatorHarness(t *testing.T) harness {
	t.Helper()
	h := newHarness(t)
	h.staff(staffFixture{modActor, adminauth.Moderator, true})
	return h
}

func TestAdminUserListPages(t *testing.T) {
	h := moderatorHarness(t)
	const total = 26*listPage + 1
	h.users(total, time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC))

	for _, tc := range []struct {
		name string
		req  usersrpc.AdminRequest
		meta pageMeta
		rows []string
	}{
		{
			name: "newest first",
			req:  usersrpc.AdminRequest{Page: 1, Limit: listPage},
			meta: pageMeta{1, listPage, 27, total, true},
			rows: seededUsernames(total-1, listPage, -1),
		},
		{
			name: "oldest first",
			req:  usersrpc.AdminRequest{Page: 2, Limit: listPage, Order: "oldest"},
			meta: pageMeta{2, listPage, 27, total, true},
			rows: seededUsernames(listPage, listPage, 1),
		},
		{
			name: "page 26 with clamped page size",
			req:  usersrpc.AdminRequest{Page: 26, Limit: 100},
			meta: pageMeta{26, listPage, 27, total, true},
			rows: seededUsernames(listPage, listPage, -1),
		},
		{
			name: "past the last page",
			req:  usersrpc.AdminRequest{Page: 999, Limit: listPage},
			meta: pageMeta{27, listPage, 27, total, false},
			rows: seededUsernames(0, 1, -1),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := h.admin(t, "list", modActor, tc.req)

			require.Empty(t, reply.Error)
			assert.Equal(t, tc.meta, metaOf(reply))
			assert.Equal(t, tc.rows, usernames(reply))
		})
	}
}

func seedDirectoryFixtures(t *testing.T, h harness) {
	t.Helper()
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	for i, row := range []struct {
		name   string
		status user.Status
		active bool
		banned bool
	}{
		{"needle-vip", user.StatusVip, true, false},
		{"needle-paid", user.StatusPaid, true, false},
		{"needle-free", user.StatusFree, true, false},
		{"needle-inactive", user.StatusVip, false, false},
		{"needle-banned", user.StatusPaid, false, true},
		{"another-paid", user.StatusPaid, true, false},
	} {
		h.client.User.Create().SetID(uint64(6001 + i)).
			SetUsername(row.name).SetEmail(row.name + "@example.invalid").
			SetStatus(row.status).SetIsActive(row.active).SetBanned(row.banned).
			SetCreatedAt(base.Add(time.Duration(i) * time.Minute)).ExecX(t.Context())
	}
}

func TestAdminUserListCountsFilteredRows(t *testing.T) {
	h := moderatorHarness(t)
	seedDirectoryFixtures(t, h)

	for _, tc := range []struct {
		name string
		req  usersrpc.AdminRequest
		meta pageMeta
		ids  []uint64
	}{
		{
			name: "case-insensitive search",
			req:  usersrpc.AdminRequest{Page: 2, Limit: 2, Search: " NEEDLE "},
			meta: pageMeta{2, 2, 3, 5, true},
			ids:  []uint64{6003, 6002},
		},
		{
			name: "vip",
			req:  usersrpc.AdminRequest{Page: 1, Limit: listPage, State: "vip"},
			meta: pageMeta{1, listPage, 1, 1, false},
			ids:  []uint64{6001},
		},
		{
			name: "paid includes only active unbanned users",
			req:  usersrpc.AdminRequest{Page: 1, Limit: listPage, State: "paid"},
			meta: pageMeta{1, listPage, 1, 2, false},
			ids:  []uint64{6006, 6002},
		},
		{
			name: "free",
			req:  usersrpc.AdminRequest{Page: 1, Limit: listPage, State: "free"},
			meta: pageMeta{1, listPage, 1, 1, false},
			ids:  []uint64{6003},
		},
		{
			name: "inactive excludes banned users",
			req:  usersrpc.AdminRequest{Page: 1, Limit: listPage, State: "inactive"},
			meta: pageMeta{1, listPage, 1, 1, false},
			ids:  []uint64{6004},
		},
		{
			name: "banned",
			req:  usersrpc.AdminRequest{Page: 1, Limit: listPage, State: "banned"},
			meta: pageMeta{1, listPage, 1, 1, false},
			ids:  []uint64{6005},
		},
		{
			name: "live limit one counts registered users",
			req:  usersrpc.AdminRequest{Page: 1, Limit: 1, OnlyLive: true, LiveIDs: []uint64{6001, 6003, 7000}},
			meta: pageMeta{1, 1, 2, 2, true},
			ids:  []uint64{6003},
		},
		{
			name: "empty live set",
			req:  usersrpc.AdminRequest{Page: 99, Limit: listPage, OnlyLive: true},
			meta: pageMeta{1, listPage, 1, 0, false},
			ids:  []uint64{},
		},
		{
			name: "unknown state and live ids without live filter",
			req:  usersrpc.AdminRequest{Page: 1, Limit: listPage, State: "nope", LiveIDs: []uint64{6001}},
			meta: pageMeta{1, listPage, 1, 6, false},
			ids:  []uint64{6006, 6005, 6004, 6003, 6002, 6001},
		},
		{
			name: "search, state, and live intersection",
			req:  usersrpc.AdminRequest{Page: 26, Search: "needle", State: "paid", OnlyLive: true, LiveIDs: []uint64{6002, 6004, 6006, 7000}},
			meta: pageMeta{1, listPage, 1, 1, false},
			ids:  []uint64{6002},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := h.admin(t, "list", modActor, tc.req)

			require.Empty(t, reply.Error)
			assert.Equal(t, tc.meta, metaOf(reply))
			assert.Equal(t, tc.ids, userIDs(reply))
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
		staffFixture{modActor, adminauth.Moderator, true},
		staffFixture{adminActor, adminauth.Admin, true},
		staffFixture{ownerActor, adminauth.Owner, true},
	)
	actors := []uint64{modActor, adminActor, ownerActor}

	for _, tc := range []struct {
		verb string
		min  int
	}{
		{"get", 0}, {"list", 0}, {"stats", 0}, {"enrollment", 0}, {"overview", 0},
		{"token_status", 0}, {"ban", 0}, {"unban", 0},
		{"set_status", 1}, {"set_active", 1}, {"set_creator_code", 1}, {"test.set", 1},
		{"voice_usage_set", 1},
		{"reset", 1}, {"token_set", 1}, {"token_clear", 1},
		{"delete", 2},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			for rank, actor := range actors {
				reply := h.admin(t, tc.verb, actor, usersrpc.AdminRequest{UserID: absentTarget, Status: "paid"})

				assert.Equal(t, rank < tc.min, reply.Code == domainrpc.CodeForbidden, "actor %d: %s", actor, reply.Error)
			}
		})
	}
}

func TestAdminGuardRefusesUnidentifiedActors(t *testing.T) {
	h := newHarness(t)
	h.staff(
		staffFixture{modActor, adminauth.Moderator, true},
		staffFixture{inactiveMod, adminauth.Moderator, false},
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
