package rpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/enttest"
	"ItsBagelBot/app/users/ent/user"
	"ItsBagelBot/app/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupAdminRPCTest(t *testing.T) (*adminRPC, *ent.Client) {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:adminrpc?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	// packer and pub are nil because the list/search tests do not exercise
	// write or token paths that would call them.
	repo := repository.NewUsers(client, nil, nil)
	t.Cleanup(repo.Close)

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

	now := time.Now().UTC()
	// Two signups today, one yesterday, one outside the 7-day window.
	stamps := []time.Time{
		now.Add(-1 * time.Hour),
		now.Add(-2 * time.Hour),
		now.AddDate(0, 0, -1),
		now.AddDate(0, 0, -10),
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

	today := reply.Enrollment.Days[6]
	yesterday := reply.Enrollment.Days[5]
	assert.Equal(t, now.Format(time.DateOnly), today.Date)
	assert.Equal(t, 2, today.Count)
	assert.Equal(t, 1, yesterday.Count)
	// Window days without signups are present and zero-filled.
	assert.Equal(t, 0, reply.Enrollment.Days[0].Count)
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
