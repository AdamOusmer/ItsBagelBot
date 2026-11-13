// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/admin/ent/adminuser"
	"ItsBagelBot/app/db/admin/repository"
	"ItsBagelBot/app/db/admin/rollup"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/valkeytest"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAnalyticsReadsLiveCellsAlongsideHistory(t *testing.T) {
	vc := valkeytest.Spawn(t)
	h := newHarness(t, analytics.NewReader(vc))
	h.staff(staffFixture{1, adminuser.RoleAdmin, true})
	now := time.Now().UTC()
	yesterday := catalog.PeriodStart(catalog.Day, now.AddDate(0, 0, -1))
	repo := repository.New(h.client)
	_, err := repo.UpsertAnalytics(t.Context(), []repository.AnalyticsRow{
		{Window: catalog.Day, Start: yesterday, Metric: catalog.Lifecycle, Dims: []string{"signup"}, Value: 8},
		{Window: catalog.Day, Start: yesterday, Metric: catalog.AccountsTotal, Dims: []string{"paid"}, Value: 10},
		{Window: catalog.Day, Start: now, Metric: catalog.Lifecycle, Dims: []string{"signup"}, Value: 999},
	})
	require.NoError(t, err)
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithClock(func() time.Time { return yesterday }), analytics.WithoutTicker())
	rec.Add(catalog.Lifecycle, 4, "signup")
	rec.Close()
	rec = analytics.New(vc, zap.NewNop(), nil, analytics.WithClock(func() time.Time { return now }), analytics.WithoutTicker())
	rec.Add(catalog.Lifecycle, 3, "signup")
	rec.Shift(nil, analytics.Tally{catalog.Field(catalog.AccountsTotal, "free"): 7})
	for _, id := range []uint64{1, 1, 2} {
		rec.Distinct(catalog.ChannelsActive, id)
		rec.Cohort(id, now.AddDate(0, 0, -30))
	}
	rec.Close()
	week := catalog.Period(catalog.Week, now.AddDate(0, 0, -30))
	today := catalog.PeriodStart(catalog.Day, now).Format(time.DateOnly)
	for _, tc := range []struct {
		window  string
		metrics []string
		want    map[string]int64
	}{
		{"d", []string{"lifecycle", "accounts.total", "channels.active", "chat.messages"}, map[string]int64{
			yesterday.Format(time.DateOnly) + "|lifecycle|signup":    8,
			yesterday.Format(time.DateOnly) + "|accounts.total|paid": 10,
			today + "|lifecycle|signup":                              3, today + "|accounts.total|free": 7, today + "|accounts.total|paid": 0,
			today + "|channels.active": 2, today + "|chat.messages": 0,
		}},
		{"w", []string{"channels.active", "cohort.active"}, map[string]int64{
			catalog.PeriodStart(catalog.Week, now).Format(time.DateOnly) + "|channels.active":       2,
			catalog.PeriodStart(catalog.Week, now).Format(time.DateOnly) + "|cohort.active|" + week: 2,
		}},
		{"m", []string{"channels.active"}, map[string]int64{catalog.PeriodStart(catalog.Month, now).Format(time.DateOnly) + "|channels.active": 2}},
	} {
		t.Run(tc.window, func(t *testing.T) {
			reply := call[usersrpc.AdminReply](t, h, analyticsSubject, usersrpc.AdminRequest{ActorID: "1", Window: tc.window, Metrics: tc.metrics, Days: 30})
			require.Empty(t, reply.Refusal.Code)
			require.NotNil(t, reply.Analytics)
			got := map[string]int64{}
			for _, row := range reply.Analytics.Rows {
				got[row.Start+"|"+catalog.Field(catalog.Metric(row.Metric), row.Dims...)] = row.Value
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestAnalyticsRetainsPendingCellsAcrossRollup(t *testing.T) {
	vc := valkeytest.Spawn(t)
	h := newHarness(t, analytics.NewReader(vc))
	h.staff(staffFixture{1, adminuser.RoleAdmin, true})
	now := time.Now().UTC()
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithClock(func() time.Time { return now.AddDate(0, 0, -1) }), analytics.WithoutTicker())
	rec.Add(catalog.Lifecycle, 5, "signup")
	rec.Distinct(catalog.ChannelsActive, 42)
	rec.Close()
	request := usersrpc.AdminRequest{ActorID: "1", Window: "d", Metrics: []string{"lifecycle", "channels.active"}, Days: 7}
	before := call[usersrpc.AdminReply](t, h, analyticsSubject, request)
	require.Empty(t, before.Refusal.Code)
	require.NotEmpty(t, before.Analytics.Rows)
	_, err := rollup.New(vc, repository.New(h.client)).Run(t.Context(), catalog.PeriodStart(catalog.Day, now).Add(time.Hour))
	require.NoError(t, err)
	after := call[usersrpc.AdminReply](t, h, analyticsSubject, request)
	require.Equal(t, before, after)
}
