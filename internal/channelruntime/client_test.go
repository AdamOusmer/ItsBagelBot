package channelruntime

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSnapshotAdmissionSeparatesDemandFromBackground(t *testing.T) {
	active := Snapshot{Registered: true, AccountCreatedAt: 101, State: lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, AdmissionGeneration: 1}}
	require.True(t, active.AllowsRuntime())
	require.True(t, active.AllowsBackground())
	dormant := active
	dormant.State.RuntimeState = lifecycle.Dormant
	require.True(t, dormant.AllowsRuntime())
	require.False(t, dormant.AllowsBackground())
	banned := active
	banned.State.RuntimeState = lifecycle.Blocked
	banned.State.ChatRestriction = lifecycle.RestrictionBanned
	banned.State.AdmissionGeneration++
	require.False(t, banned.AllowsRuntime())
	require.False(t, banned.AllowsBackground())
	for _, blocked := range []Snapshot{{}, {Registered: true, State: lifecycle.State{}}, {Registered: true, ServiceBanned: true, State: active.State}} {
		require.False(t, blocked.AllowsRuntime())
		require.False(t, blocked.AllowsBackground())
	}
	disabled := active
	disabled.State.DesiredEnabled = false
	require.False(t, disabled.AllowsRuntime())
}
