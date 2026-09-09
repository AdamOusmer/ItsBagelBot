// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/adminuser"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupAdminRPCTest(t *testing.T) (*adminRPC, *ent.Client) {
	t.Helper()

	client := testdb.Open(t, "adminrpc", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })

	// packer and pub are nil because the list/search tests do not exercise
	// write or token paths that would call them.
	repo := repository.NewUsers(client, nil, nil, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })

	return &adminRPC{repo: repo, log: zap.NewNop()}, client
}

func createAdminUser(t *testing.T, client *ent.Client, i int, updatedAt time.Time) {
	t.Helper()

	client.User.Create().
		SetID(uint64(9000 + i)).
		SetUsername(fmt.Sprintf("user-%02d", i)).
		SetEmail(fmt.Sprintf("user-%02d@example.invalid", i)).
		SetStatus(user.StatusFree).
		SetUpdatedAt(updatedAt).
		ExecX(context.Background())
}

func TestAdminUserListPagesResults(t *testing.T) {
	a, client := setupAdminRPCTest(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		createAdminUser(t, client, i, base.Add(time.Duration(i)*time.Minute))
	}

	first := a.list(ctx, usersrpc.AdminRequest{Page: 1, Limit: adminUserPageSize})
	require.Empty(t, first.Error)
	require.Len(t, first.Users, adminUserPageSize)
	assert.Equal(t, 1, first.Page)
	assert.Equal(t, adminUserPageSize, first.PageSize)
	assert.Equal(t, adminUserMaxPages, first.MaxPages)
	assert.True(t, first.HasMore)
	assert.Equal(t, "user-29", first.Users[0].Username)
	assert.Equal(t, fmt.Sprintf("user-%02d", 30-adminUserPageSize), first.Users[adminUserPageSize-1].Username)

	second := a.list(ctx, usersrpc.AdminRequest{Page: 2, Limit: adminUserPageSize})
	require.Empty(t, second.Error)
	require.Len(t, second.Users, adminUserPageSize)
	assert.False(t, second.HasMore)
	assert.Equal(t, fmt.Sprintf("user-%02d", 30-adminUserPageSize-1), second.Users[0].Username)
}

func TestAdminUserListSearchesBeforePaging(t *testing.T) {
	a, client := setupAdminRPCTest(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		createAdminUser(t, client, i, base.Add(time.Duration(i)*time.Minute))
	}
	client.User.Create().
		SetID(424242).
		SetUsername("needle-user").
		SetEmail("needle@example.invalid").
		SetStatus(user.StatusVip).
		SetUpdatedAt(base.Add(2 * time.Hour)).
		ExecX(ctx)

	reply := a.list(ctx, usersrpc.AdminRequest{Page: 1, Limit: adminUserPageSize, Search: "NEEDLE"})
	require.Empty(t, reply.Error)
	require.Len(t, reply.Users, 1)
	assert.Equal(t, "needle-user", reply.Users[0].Username)
	assert.Equal(t, uint64(424242), reply.Users[0].ID)
	assert.False(t, reply.HasMore)
}

func TestAdminEnrollmentBucketsPerDay(t *testing.T) {
	a, client := setupAdminRPCTest(t)
	ctx := context.Background()

	fixtureDay := time.Now().UTC().Truncate(24 * time.Hour)
	// Two signups today, one yesterday, one outside the 7-day window.
	stamps := []time.Time{
		fixtureDay,
		fixtureDay.Add(time.Second),
		fixtureDay.AddDate(0, 0, -1),
		fixtureDay.AddDate(0, 0, -10),
	}
	for i, ts := range stamps {
		client.User.Create().
			SetID(uint64(7000 + i)).
			SetUsername(fmt.Sprintf("enroll-%02d", i)).
			SetEmail(fmt.Sprintf("enroll-%02d@example.invalid", i)).
			SetStatus(user.StatusFree).
			SetCreatedAt(ts).
			SetUpdatedAt(ts).
			ExecX(ctx)
	}

	reply := a.enrollment(ctx, usersrpc.AdminRequest{Days: 7})
	require.Empty(t, reply.Error)
	require.NotNil(t, reply.Enrollment)
	require.Len(t, reply.Enrollment.Days, 7)

	countsByDate := make(map[string]int, len(reply.Enrollment.Days))
	for _, day := range reply.Enrollment.Days {
		countsByDate[day.Date] = day.Count
	}
	fixtureDate := fixtureDay.Format(time.DateOnly)
	yesterdayDate := fixtureDay.AddDate(0, 0, -1).Format(time.DateOnly)
	emptyDate := fixtureDay.AddDate(0, 0, -2).Format(time.DateOnly)
	require.Contains(t, countsByDate, fixtureDate)
	require.Contains(t, countsByDate, yesterdayDate)
	require.Contains(t, countsByDate, emptyDate)
	assert.Equal(t, 2, countsByDate[fixtureDate])
	assert.Equal(t, 1, countsByDate[yesterdayDate])
	// Window days without signups are present and zero-filled.
	assert.Equal(t, 0, countsByDate[emptyDate])
	// Totals cover the whole base, including rows outside the window.
	assert.Equal(t, 4, reply.Enrollment.Stats.TotalUsers)
}

func TestAdminEnrollmentDefaultsAndClampsWindow(t *testing.T) {
	a, _ := setupAdminRPCTest(t)
	ctx := context.Background()

	byDefault := a.enrollment(ctx, usersrpc.AdminRequest{})
	require.Empty(t, byDefault.Error)
	require.NotNil(t, byDefault.Enrollment)
	assert.Len(t, byDefault.Enrollment.Days, enrollmentDefaultDays)

	clamped := a.enrollment(ctx, usersrpc.AdminRequest{Days: 500})
	require.Empty(t, clamped.Error)
	require.NotNil(t, clamped.Enrollment)
	assert.Len(t, clamped.Enrollment.Days, enrollmentMaxDays)
}

func TestAdminUserListFiltersByState(t *testing.T) {
	a, client := setupAdminRPCTest(t)
	ctx := context.Background()
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
		{6004, user.StatusVip, false, false}, // inactive beats tier
		{6005, user.StatusPaid, false, true}, // banned beats everything
	}
	for i, s := range seed {
		client.User.Create().
			SetID(s.id).
			SetUsername(fmt.Sprintf("state-%02d", i)).
			SetEmail(fmt.Sprintf("state-%02d@example.invalid", i)).
			SetStatus(s.status).
			SetIsActive(s.active).
			SetBanned(s.banned).
			SetUpdatedAt(base.Add(time.Duration(i) * time.Minute)).
			ExecX(ctx)
	}

	cases := map[string][]uint64{
		"vip":      {6001},
		"paid":     {6002},
		"free":     {6003},
		"inactive": {6004},
		"banned":   {6005},
	}
	for state, want := range cases {
		reply := a.list(ctx, usersrpc.AdminRequest{Page: 1, Limit: adminUserPageSize, State: state})
		require.Empty(t, reply.Error, state)
		ids := make([]uint64, 0, len(reply.Users))
		for _, u := range reply.Users {
			ids = append(ids, u.ID)
		}
		assert.Equal(t, want, ids, state)
	}

	// Unknown state applies no filter.
	all := a.list(ctx, usersrpc.AdminRequest{Page: 1, Limit: adminUserPageSize, State: "nope"})
	require.Empty(t, all.Error)
	assert.Len(t, all.Users, len(seed))
}

// ── Role ladder ─────────────────────────────────────────────────────────────

// staffedAdminRPC is the admin surface wired to a real staff table, which the
// role tests need and the list/search tests do not.
func staffedAdminRPC(t *testing.T) (*adminRPC, *ent.Client) {
	t.Helper()
	a, client := setupAdminRPCTest(t)
	a.gate = staffGate{db: client}
	return a, client
}

const (
	modActor     = uint64(7001)
	adminActor   = uint64(7002)
	ownerActor   = uint64(7003)
	inactiveMod  = uint64(7004)
	absentTarget = "999999"
)

// handlerFor returns the guarded handler bound to one verb name, i.e. exactly
// what SubscribeAdmin binds to the subject, guard included.
func handlerFor(t *testing.T, a *adminRPC, verb string) func(context.Context, usersrpc.AdminRequest) usersrpc.AdminReply {
	t.Helper()
	for _, v := range a.verbs() {
		if v.name == verb {
			return a.guarded(v)
		}
	}
	t.Fatalf("no admin verb named %q", verb)
	return nil
}

// TestAdminVerbRoleLadder drives every guarded verb through the ladder.
//
// The allowed cases address a user id that does not exist, so the assertion is
// "the guard let this through" (not_found from the handler) rather than
// "the write succeeded": the write paths publish over a nil bus in this
// harness, and what is under test here is authorization, not persistence.
func TestAdminVerbRoleLadder(t *testing.T) {
	a, client := staffedAdminRPC(t)
	createStaff(t, client, staffFixture{id: modActor, role: adminuser.RoleModerator, active: true})
	createStaff(t, client, staffFixture{id: adminActor, role: adminuser.RoleAdmin, active: true})
	createStaff(t, client, staffFixture{id: ownerActor, role: adminuser.RoleOwner, active: true})
	createStaff(t, client, staffFixture{id: inactiveMod, role: adminuser.RoleModerator, active: false})

	cases := []struct {
		name    string
		verb    string
		actor   string
		allowed bool
	}{
		{"moderator may ban", "ban", fmt.Sprint(modActor), true},
		{"moderator may read", "get", fmt.Sprint(modActor), true},
		{"moderator may not set status", "set_status", fmt.Sprint(modActor), false},
		{"moderator may not delete", "delete", fmt.Sprint(modActor), false},
		{"admin may set status", "set_status", fmt.Sprint(adminActor), true},
		{"admin may not delete", "delete", fmt.Sprint(adminActor), false},
		{"owner may delete", "delete", fmt.Sprint(ownerActor), true},
		{"missing actor is refused", "get", "", false},
		{"unknown actor is refused", "get", "424242", false},
		{"inactive actor is refused", "get", fmt.Sprint(inactiveMod), false},
		{"non-numeric actor is refused", "ban", "not-an-id", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := handlerFor(t, a, tc.verb)(context.Background(), usersrpc.AdminRequest{
				ActorID: tc.actor,
				UserID:  absentTarget,
				Status:  "paid",
			})
			if tc.allowed {
				assert.NotEqual(t, domainrpc.CodeForbidden, reply.Code, "guard refused an allowed call: %s", reply.Error)
				return
			}
			assert.Equal(t, domainrpc.CodeForbidden, reply.Code)
			assert.NotEmpty(t, reply.Error)
		})
	}
}

// TestAdminVerbsCoverEverySubject pins the registry: a verb added without a
// role row cannot exist (the struct requires one), and this asserts the set of
// subjects the console depends on is still bound.
func TestAdminVerbsCoverEverySubject(t *testing.T) {
	a, _ := staffedAdminRPC(t)
	got := make(map[string]adminuser.Role, len(a.verbs()))
	for _, v := range a.verbs() {
		got[v.name] = v.min
	}
	assert.Equal(t, map[string]adminuser.Role{
		"get":              adminuser.RoleModerator,
		"list":             adminuser.RoleModerator,
		"stats":            adminuser.RoleModerator,
		"enrollment":       adminuser.RoleModerator,
		"overview":         adminuser.RoleModerator,
		"token_status":     adminuser.RoleModerator,
		"ban":              adminuser.RoleModerator,
		"unban":            adminuser.RoleModerator,
		"set_status":       adminuser.RoleAdmin,
		"set_active":       adminuser.RoleAdmin,
		"set_creator_code": adminuser.RoleAdmin,
		"reset":            adminuser.RoleAdmin,
		"token_set":        adminuser.RoleAdmin,
		"token_clear":      adminuser.RoleAdmin,
		"delete":           adminuser.RoleOwner,
	}, got)
}

// TestInternalGetAnswersWithoutActor pins the pair SubscribeAdmin binds: the
// `get` verb refuses a caller that names no operator, and the same handler on
// bagel.rpc.internal.users.get answers it. The service callers (transactions'
// gift recipient vetting, notifications' target resolution) send no actor_id
// at all, so a regression that gated the internal subject would break them
// silently -- the broker would return the refusal, not an error.
func TestInternalGetAnswersWithoutActor(t *testing.T) {
	a, _ := staffedAdminRPC(t)
	req := usersrpc.AdminRequest{UserID: absentTarget}

	guarded := handlerFor(t, a, "get")(context.Background(), req)
	assert.Equal(t, domainrpc.CodeForbidden, guarded.Code)

	// a.get is what SubscribeAdmin binds to the internal subject, unwrapped by
	// guarded(): no role is consulted, so the reply is the store's answer for
	// a user that does not exist rather than a refusal to ask.
	internal := a.get(context.Background(), req)
	assert.NotEqual(t, domainrpc.CodeForbidden, internal.Code)
	assert.Equal(t, domainrpc.CodeNotFound, internal.Code)
	assert.Nil(t, internal.User)
}
