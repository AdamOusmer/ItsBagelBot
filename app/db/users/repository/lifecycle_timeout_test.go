package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLifecycleTimeoutPausesWithoutIncidentOrCleanup(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy}
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "timeout", EventID: "timeout", ObservedAt: now, TimeoutUntil: now.Add(10 * time.Second)}, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, s.AllowsRuntime())
	require.Empty(t, s.IncidentID, "a timeout must not notify the owner")
	require.True(t, s.NormalCleanupAt.IsZero(), "a timeout must not tear down subscriptions")
}
