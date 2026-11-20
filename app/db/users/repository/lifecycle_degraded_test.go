package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDegradedChannelRetriesThreeTimesThenWaitsForAGrant(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Degraded, AdmissionGeneration: 4, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, NextAttemptAt: now}
	step := func(action string, at time.Time) bool {
		changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: action, OperationID: s.OperationID, AdmissionGeneration: s.AdmissionGeneration}, at, false)
		require.NoError(t, err)
		return changed
	}
	retries := 0
	for !s.NextAttemptAt.IsZero() && retries < 10 {
		require.True(t, step("degraded_retry", s.NextAttemptAt))
		s.Attempts = lifecycle.MaxRecoveryAttempts
		require.True(t, step("recovery_failed", s.NextAttemptAt))
		retries++
	}
	resumed := step("grant_saved", now)
	require.Equal(t, []any{3, true, lifecycle.Recovering, 0}, []any{retries, resumed, s.RuntimeState, s.DegradedRetries})
}

func TestGrantSavedLeavesAScheduledRetryAlone(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Degraded, AdmissionGeneration: 4, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, NextAttemptAt: now.Add(time.Hour)}
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "grant_saved"}, now, false)
	require.NoError(t, err)
	require.Equal(t, []any{false, lifecycle.Degraded}, []any{changed, s.RuntimeState})
}

func TestLifecycleBlockedDegradedRetryLeavesTheDueIndex(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Degraded, AdmissionGeneration: 4, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, NextAttemptAt: now}
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "degraded_retry"}, now, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, lifecycle.Degraded, s.RuntimeState)
	require.True(t, s.NextAttemptAt.After(now), "a service-banned account must not stay due every tick")
}
