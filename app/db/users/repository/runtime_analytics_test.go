// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent/devicegrant"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"github.com/stretchr/testify/require"
)

func TestRuntimeAndDesktopSeedOnlyCountsEligibleRows(t *testing.T) {
	client, _, repo := setup(t)
	for i, state := range append(catalog.Enum(catalog.KindRuntime), "private-input", "active", "active") {
		client.User.Create().SetID(uint64(i + 1)).SetUsername(fmt.Sprint("viewer", i)).SetEmail(fmt.Sprint(i, "@test.invalid")).
			SetLifecycleState(state).SetTestAccount(i == 7).SetBanned(i == 8).ExecX(t.Context())
	}
	now := time.Now()
	for i, row := range []struct {
		platform          devicegrant.Platform
		owner             uint64
		sliding, absolute time.Duration
		revoked           bool
	}{
		{devicegrant.PlatformMacos, 1, time.Hour, time.Hour, false},
		{devicegrant.PlatformWindows, 2, time.Hour, time.Hour, false},
		{devicegrant.PlatformLinux, 3, time.Hour, time.Hour, false},
		{devicegrant.PlatformMacos, 1, -time.Hour, time.Hour, false},
		{devicegrant.PlatformMacos, 1, time.Hour, -time.Hour, false},
		{devicegrant.PlatformMacos, 1, time.Hour, time.Hour, true},
		{devicegrant.PlatformMacos, 8, time.Hour, time.Hour, false},
		{devicegrant.PlatformMacos, 9, time.Hour, time.Hour, false},
	} {
		grant := client.DeviceGrant.Create().SetUserID(row.owner).SetGrantID(fmt.Sprint("grant", i)).SetDeviceID(fmt.Sprint("device", i)).
			SetPlatform(row.platform).SetRefreshHash([]byte(fmt.Sprint(i))).
			SetRefreshSlidingExpiresAt(now.Add(row.sliding)).SetRefreshAbsoluteExpiresAt(now.Add(row.absolute))
		if row.revoked {
			grant.SetRevokedAt(now)
		}
		grant.ExecX(t.Context())
	}
	got, err := repo.AnalyticsSeed(t.Context())
	require.NoError(t, err)
	actual := analytics.Tally{}
	for _, metric := range []catalog.Metric{catalog.AccountsRuntime, catalog.DesktopGrants} {
		spec, _ := catalog.Lookup(metric)
		for _, dim := range catalog.Enum(spec.Dims[0]) {
			field := catalog.Field(metric, dim)
			actual[field] = got[field]
		}
	}
	require.Equal(t, analytics.Tally{
		"accounts.runtime|active": 1, "accounts.runtime|dormant": 1, "accounts.runtime|blocked": 1,
		"accounts.runtime|disabled": 1, "accounts.runtime|recovering": 1, "accounts.runtime|degraded": 1,
		"desktop.grants|macos": 1, "desktop.grants|windows": 1, "desktop.grants|linux": 1,
	}, actual)
}
