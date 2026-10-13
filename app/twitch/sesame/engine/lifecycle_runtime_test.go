package engine

import (
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/lifecycle"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"
	"context"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
	"time"
)

func lifecycleTestState(runtime string, generation int64) lifecycle.State {
	return lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: runtime, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, AdmissionGeneration: generation}
}
func bindLifecycleRuntime(t *testing.T, clientStore *projection.Store, id uint64, state lifecycle.State) *channelruntime.Client {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, clientStore.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 101, StateRevision: 1, Status: "free", IsActive: state.AllowsRuntime(), Lifecycle: &state}))
	return channelruntime.NewClient(clientStore, nil, "")
}
func TestLifecycleBanStopsArmedOfflineConfirmations(t *testing.T) {
	f := newRecheckFixture(t, nil, time.Minute, 2*time.Minute)
	ctx := context.Background()
	projectionStore := projection.NewStore(f.client)
	t.Cleanup(func() {
		f.client.Do(ctx, f.client.B().Del().Key("settings:"+strconv.FormatUint(f.id, 10), watchtime.AdmissionKey(f.id), "live:confirm:admission:"+strconv.FormatUint(f.id, 10)).Build())
	})
	active := lifecycleTestState(lifecycle.Active, 1)
	f.store.cfg.Runtime = bindLifecycleRuntime(t, projectionStore, f.id, active)
	applied, err := f.store.ClearLive(ctx, f.id, livekey.VersionNow())
	require.NoError(t, err)
	require.True(t, applied)
	require.True(t, f.exists(t, offlineConfirmKey(0, f.id)))
	require.True(t, f.exists(t, offlineConfirmKey(1, f.id)))
	banned := lifecycleTestState(lifecycle.Blocked, 2)
	banned.ChatRestriction = lifecycle.RestrictionBanned
	require.NoError(t, projectionStore.SetLifecycle(ctx, f.id, projection.LifecycleProjection{State: banned, AccountCreatedAt: 101, StateRevision: 2}))
	f.store.onExpired(ctx, offlineConfirmKey(0, f.id))
	f.store.onExpired(ctx, offlineConfirmKey(1, f.id))
	f.store.onExpired(ctx, liveKey(f.id))
	require.Empty(t, f.rechecks(), "queued false-offline corrections must not admit Twitch jobs after ban")
	recovered := lifecycleTestState(lifecycle.Active, 3)
	require.NoError(t, projectionStore.SetLifecycle(ctx, f.id, projection.LifecycleProjection{State: recovered, AccountCreatedAt: 101, StateRevision: 3}))
	f.store.onExpired(ctx, offlineConfirmKey(0, f.id))
	f.store.onExpired(ctx, offlineConfirmKey(1, f.id))
	require.Empty(t, f.rechecks(), "unban cannot admit stages queued by the retired generation")
	require.NoError(t, projectionStore.SetLifecycle(ctx, f.id, projection.LifecycleProjection{State: banned, AccountCreatedAt: 101, StateRevision: 4}))
	live, err := f.store.IsLive(ctx, f.id)
	require.NoError(t, err)
	require.False(t, live)
	require.Empty(t, f.rechecks())
}
func TestLifecycleAlreadyBannedDoesNotArmOfflineWork(t *testing.T) {
	f := newRecheckFixture(t, nil, time.Minute, 2*time.Minute)
	ctx := context.Background()
	p := projection.NewStore(f.client)
	t.Cleanup(func() {
		f.client.Do(ctx, f.client.B().Del().Key("settings:"+strconv.FormatUint(f.id, 10), watchtime.AdmissionKey(f.id), "live:confirm:admission:"+strconv.FormatUint(f.id, 10)).Build())
	})
	banned := lifecycleTestState(lifecycle.Blocked, 2)
	banned.ChatRestriction = lifecycle.RestrictionBanned
	f.store.cfg.Runtime = bindLifecycleRuntime(t, p, f.id, banned)
	applied, err := f.store.ClearLive(ctx, f.id, livekey.VersionNow())
	require.NoError(t, err)
	require.True(t, applied, "stream facts stay independently ordered")
	require.False(t, f.exists(t, offlineConfirmKey(0, f.id)))
	require.False(t, f.exists(t, offlineConfirmKey(1, f.id)))
	require.Empty(t, f.rechecks())
}
func TestLifecycleDormantOfflineConfirmationsRemainAdmitted(t *testing.T) {
	f := newRecheckFixture(t, nil, time.Minute, 2*time.Minute)
	ctx := context.Background()
	p := projection.NewStore(f.client)
	t.Cleanup(func() {
		f.client.Do(ctx, f.client.B().Del().Key("settings:"+strconv.FormatUint(f.id, 10), watchtime.AdmissionKey(f.id), "live:confirm:admission:"+strconv.FormatUint(f.id, 10)).Build())
	})
	dormant := lifecycleTestState(lifecycle.Dormant, 7)
	f.store.cfg.Runtime = bindLifecycleRuntime(t, p, f.id, dormant)
	applied, err := f.store.ClearLive(ctx, f.id, livekey.VersionNow())
	require.NoError(t, err)
	require.True(t, applied)
	require.True(t, f.exists(t, offlineConfirmKey(0, f.id)))
	require.True(t, f.exists(t, offlineConfirmKey(1, f.id)))
	f.store.onExpired(ctx, offlineConfirmKey(0, f.id))
	f.store.onExpired(ctx, offlineConfirmKey(1, f.id))
	jobs := f.rechecks()
	require.Len(t, jobs, 2)
	for _, job := range jobs {
		require.Equal(t, int64(101), job.AccountCreatedAt)
		require.Equal(t, int64(7), job.AdmissionGeneration)
	}
}
func TestLifecycleTimerGenerationRejectsPreBanScheduleAfterUnban(t *testing.T) {
	f := newGateStoreFixture(t, fakeReader{})
	ctx := context.Background()
	p := projection.NewStore(f.store.client)
	t.Cleanup(func() {
		f.store.client.Do(ctx, f.store.client.B().Del().Key("settings:"+strconv.FormatUint(f.bid, 10), watchtime.AdmissionKey(f.bid), f.ref("t1").admissionKey()).Build())
	})
	active := lifecycleTestState(lifecycle.Active, 1)
	f.store.runtime = bindLifecycleRuntime(t, p, f.bid, active)
	td := timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true}
	f.withConfig(t, td)
	f.store.armAfter(ctx, f.armed("t1", td), time.Minute)
	_, ok := f.store.resolveArmed(ctx, f.ref("t1"))
	require.True(t, ok)
	banned := lifecycleTestState(lifecycle.Blocked, 2)
	banned.ChatRestriction = lifecycle.RestrictionBanned
	require.NoError(t, p.SetLifecycle(ctx, f.bid, projection.LifecycleProjection{State: banned, AccountCreatedAt: 101, StateRevision: 2}))
	_, ok = f.store.resolveArmed(ctx, f.ref("t1"))
	require.False(t, ok)
	recovered := lifecycleTestState(lifecycle.Active, 3)
	require.NoError(t, p.SetLifecycle(ctx, f.bid, projection.LifecycleProjection{State: recovered, AccountCreatedAt: 101, StateRevision: 3}))
	_, ok = f.store.resolveArmed(ctx, f.ref("t1"))
	require.False(t, ok, "unban must not resurrect the pre-ban timer schedule")
	f.store.armAfter(ctx, f.armed("t1", td), time.Minute)
	armed, ok := f.store.resolveArmed(ctx, f.ref("t1"))
	require.True(t, ok)
	require.Equal(t, int64(3), armed.admission.State.AdmissionGeneration)
	dormant := lifecycleTestState(lifecycle.Dormant, 4)
	require.NoError(t, p.SetLifecycle(ctx, f.bid, projection.LifecycleProjection{State: dormant, AccountCreatedAt: 101, StateRevision: 4}))
	_, ok = f.store.resolveArmed(ctx, f.ref("t1"))
	require.False(t, ok, "dormant channels do not execute scheduled output")
}
