// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/internal/analytics"
	"github.com/stretchr/testify/require"
)

func TestTimerLevelsReadEnabledConfigurations(t *testing.T) {
	client, _, repo := setup(t)
	for i, row := range []struct {
		enabled bool
		config  string
	}{
		{true, `{"timers":[{"enabled":true},{"enabled":false},{"enabled":true,"minChatLines":4,"maxFiresPerStream":5,"endsAt":"2030-01-01T00:00:00Z","allowOffline":true},{"enabled":true,"minChatLines":-1,"maxFiresPerStream":-2}]}`},
		{false, `{"timers":[{"enabled":true,"allowOffline":true}]}`},
		{true, `broken`},
	} {
		client.Modules.Create().SetUserID(uint64(i + 1)).SetName("timers").SetIsEnabled(row.enabled).
			SetConfigs([]byte(row.config)).SetRevision(0).ExecX(t.Context())
	}
	got, err := repo.AnalyticsSeed(t.Context())
	require.NoError(t, err)
	want := analytics.Tally{"timers.total|plain": 2, "timers.total|gated": 1, "timers.total|capped": 1, "timers.total|ends": 1, "timers.total|offline": 1}
	actual := analytics.Tally{}
	for field := range want {
		actual[field] = got[field]
	}
	require.Equal(t, want, actual)
}
