package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLifecycleBlockedDegradedRetryLeavesTheDueIndex(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Degraded, AdmissionGeneration: 4, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, NextAttemptAt: now}
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "degraded_retry"}, now, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, lifecycle.Degraded, s.RuntimeState)
	require.True(t, s.NextAttemptAt.After(now), "a service-banned account must not stay due every tick")
}
