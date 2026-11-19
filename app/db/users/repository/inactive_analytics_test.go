// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"fmt"
	"testing"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/lifecycle"
	"github.com/stretchr/testify/require"
)

func TestInactiveSeedSplitsEveryAccountWithoutABot(t *testing.T) {
	client, _, repo := setup(t)
	state := func(restriction, grant string, desired bool) *lifecycle.State {
		return &lifecycle.State{ChatRestriction: restriction, GrantHealth: grant, DesiredEnabled: desired}
	}
	healthy := state(lifecycle.RestrictionNone, lifecycle.GrantHealthy, true)
	for i, row := range []struct {
		state                *lifecycle.State
		active, test, banned bool
	}{
		{state: healthy, active: true},
		{state: state(lifecycle.RestrictionBanned, lifecycle.GrantHealthy, true), active: true},
		{state: nil},
		{state: state(lifecycle.RestrictionNone, lifecycle.GrantHealthy, false)},
		{state: state(lifecycle.RestrictionBanned, lifecycle.GrantHealthy, true)},
		{state: state(lifecycle.RestrictionBanned, lifecycle.GrantMissing, true)},
		{state: healthy, banned: true},
		{state: state(lifecycle.RestrictionNone, lifecycle.GrantMissing, true)},
		{state: state(lifecycle.RestrictionNone, lifecycle.GrantMissing, false), test: true},
		{state: state(lifecycle.RestrictionTimeout, lifecycle.GrantHealthy, true)},
	} {
		create := client.User.Create().SetID(uint64(i + 1)).SetUsername(fmt.Sprint("viewer", i)).SetEmail(fmt.Sprint(i, "@test.invalid")).
			SetIsActive(row.active).SetTestAccount(row.test).SetBanned(row.banned)
		if row.state != nil {
			create.SetLifecycle(row.state)
		}
		create.ExecX(t.Context())
	}
	got, err := repo.AnalyticsSeed(t.Context())
	require.NoError(t, err)
	actual := analytics.Tally{}
	for _, dim := range catalog.Enum(catalog.KindInactive) {
		field := catalog.Field(catalog.AccountsInactive, dim)
		actual[field] = got[field]
	}
	require.Equal(t, analytics.Tally{
		"accounts.inactive|banned": 3, "accounts.inactive|revoked": 2, "accounts.inactive|disabled": 2, "accounts.inactive|other": 1,
	}, actual)
	require.EqualValues(t, 10, got["accounts.total|free"])
	require.EqualValues(t, 2, got["accounts.bot_enabled|free"])
}
