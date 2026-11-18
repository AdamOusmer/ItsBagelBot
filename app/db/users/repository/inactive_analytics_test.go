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

func TestInactiveSeedCountsBansAndRevokedAccess(t *testing.T) {
	client, _, repo := setup(t)
	healthy := lifecycle.State{ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy}
	banned := lifecycle.State{ChatRestriction: lifecycle.RestrictionBanned, GrantHealth: lifecycle.GrantHealthy}
	timedOut := lifecycle.State{ChatRestriction: lifecycle.RestrictionTimeout, GrantHealth: lifecycle.GrantHealthy}
	revoked := lifecycle.State{ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantMissing}
	both := lifecycle.State{ChatRestriction: lifecycle.RestrictionBanned, GrantHealth: lifecycle.GrantMissing}
	for i, row := range []struct {
		state        *lifecycle.State
		test, banned bool
	}{
		{state: &healthy}, {state: nil}, {state: &banned}, {state: &timedOut}, {state: &revoked}, {state: &both},
		{state: &healthy, banned: true}, {state: &revoked, test: true},
	} {
		create := client.User.Create().SetID(uint64(i + 1)).SetUsername(fmt.Sprint("viewer", i)).SetEmail(fmt.Sprint(i, "@test.invalid")).
			SetTestAccount(row.test).SetBanned(row.banned)
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
	require.Equal(t, analytics.Tally{"accounts.inactive|banned": 3, "accounts.inactive|revoked": 1}, actual)
}
