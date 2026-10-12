package projection

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/watchtime"
	"github.com/stretchr/testify/require"
	"testing"
)

func runtimeState(state string, generation int64) lifecycle.State {
	return lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: state, GrantHealth: lifecycle.GrantHealthy, ChatRestriction: lifecycle.RestrictionNone, AdmissionGeneration: generation}
}
func TestLifecycleFenceRejectsStaleHydrationAndTransition(t *testing.T) {
	f := newLiveFixture(t)
	key := "settings:" + f.member()
	admission := watchtime.AdmissionKey(f.user)
	t.Cleanup(func() { f.client.Do(f.ctx, f.client.B().Del().Key(key, admission).Build()) })
	active := runtimeState(lifecycle.Active, 1)
	require.NoError(t, f.store.SetUser(f.ctx, f.user, UserProjection{AccountCreatedAt: 101, StateRevision: 1, Status: "paid", IsActive: true, Lifecycle: &active}))
	banned := runtimeState(lifecycle.Blocked, 2)
	banned.ChatRestriction = lifecycle.RestrictionBanned
	require.NoError(t, f.store.SetLifecycle(f.ctx, f.user, banned, 101, 2))
	require.NoError(t, f.store.SetUser(f.ctx, f.user, UserProjection{AccountCreatedAt: 101, StateRevision: 1, Status: "paid", IsActive: true, Lifecycle: &active}))
	require.NoError(t, f.store.SetLifecycle(f.ctx, f.user, active, 101, 1))
	got, found, err := f.store.GetUserRuntime(f.ctx, f.user)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, banned, *got.Lifecycle)
	require.False(t, got.IsActive)
	epoch, err := f.client.Do(f.ctx, f.client.B().Hget().Key(admission).Field("epoch").Build()).AsInt64()
	require.NoError(t, err)
	banned.LastFailure = "still banned"
	require.NoError(t, f.store.SetLifecycle(f.ctx, f.user, banned, 101, 3))
	after, err := f.client.Do(f.ctx, f.client.B().Hget().Key(admission).Field("epoch").Build()).AsInt64()
	require.NoError(t, err)
	require.Equal(t, epoch, after, "read-model-only updates cannot invalidate admitted operations")
}
func TestLifecycleDeletionCannotAttachToRecreatedIncarnation(t *testing.T) {
	f := newLiveFixture(t)
	t.Cleanup(func() {
		f.client.Do(f.ctx, f.client.B().Del().Key("settings:"+f.member(), watchtime.AdmissionKey(f.user)).Build())
	})
	old := runtimeState(lifecycle.Active, 1)
	require.NoError(t, f.store.SetUser(f.ctx, f.user, UserProjection{AccountCreatedAt: 101, StateRevision: 1, Status: "paid", IsActive: true, Lifecycle: &old}))
	deleted, err := f.store.DeleteAccount(f.ctx, f.user, 101)
	require.NoError(t, err)
	require.True(t, deleted)
	require.NoError(t, f.store.SetLifecycle(f.ctx, f.user, old, 101, 99))
	_, found, err := f.store.GetUserRuntime(f.ctx, f.user)
	require.NoError(t, err)
	require.False(t, found)
	replacement := runtimeState(lifecycle.Disabled, 1)
	replacement.DesiredEnabled = false
	require.NoError(t, f.store.SetUser(f.ctx, f.user, UserProjection{AccountCreatedAt: 202, StateRevision: 1, Status: "free", Lifecycle: &replacement}))
	require.NoError(t, f.store.SetLifecycle(f.ctx, f.user, old, 101, 100))
	require.NoError(t, f.store.SetUser(f.ctx, f.user, UserProjection{AccountCreatedAt: 101, StateRevision: 101, Status: "paid", IsActive: true, Lifecycle: &old}))
	deleted, err = f.store.DeleteAccount(f.ctx, f.user, 101)
	require.NoError(t, err)
	require.False(t, deleted)
	got, found, err := f.store.GetUserRuntime(f.ctx, f.user)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(202), got.AccountCreatedAt)
	require.Equal(t, replacement, *got.Lifecycle)
}
