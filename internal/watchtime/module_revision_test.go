// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime_test

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type moduleRevisionFixture struct {
	client valkey.Client
	proj   *projection.Store
	store  *watchtime.Store
	id     uint64
	sid    string
	user   projection.UserProjection
}

func newModuleRevisionFixture(t *testing.T) moduleRevisionFixture {
	t.Helper()
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR requires isolated real Valkey")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	require.NoError(t, err)
	id := uint64(time.Now().UnixNano()%100000000 + 8500000000)
	sid := strconv.FormatUint(id, 10)
	t.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key("settings:"+sid, watchtime.AdmissionKey(id), "live:"+sid, "loyaltick:state:"+sid, "loyaltick:claim:"+sid).Build())
		client.Close()
	})
	f := moduleRevisionFixture{client: client, proj: projection.NewStore(client), store: watchtime.NewStore(client), id: id, sid: sid, user: projection.UserProjection{AccountCreatedAt: 100, StateRevision: 1, IsActive: true, Status: "paid"}}
	require.NoError(t, f.proj.SetUser(t.Context(), id, f.user))
	require.NoError(t, client.Do(t.Context(), client.B().Set().Key("live:"+sid).Value("s").Build()).Error())
	return f
}

func (f moduleRevisionFixture) expire(t *testing.T) {
	t.Helper()
	require.NoError(t, f.client.Do(t.Context(), f.client.B().Del().Key("settings:"+f.sid).Build()).Error())
}

func TestLoyaltyRevisionSurvivesSettingsExpiry(t *testing.T) {
	for _, path := range []string{"event", "hydration"} {
		for _, oldRevision := range []int{0, 1} {
			for _, enabled := range []bool{false, true} {
				t.Run(path+"/old="+strconv.Itoa(oldRevision)+"/current_enabled="+strconv.FormatBool(enabled), func(t *testing.T) {
					f := newModuleRevisionFixture(t)
					ctx := t.Context()
					current := projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: enabled, Revision: 2, Configs: codec.RawMessage(`{"watchPointsPerTick":5}`)}
					require.NoError(t, f.proj.SetModule(ctx, f.id, current))
					f.expire(t)
					stale := current
					stale.Revision, stale.IsEnabled, stale.Configs = oldRevision, true, codec.RawMessage(`{"watchPointsPerTick":999}`)
					if path == "event" {
						require.NoError(t, f.proj.SetModule(ctx, f.id, stale))
					} else {
						require.NoError(t, f.proj.SetModulesWithTTL(ctx, f.id, []projection.ModuleView{stale, {Name: "timers", Revision: 1, IsEnabled: true}}, time.Minute))
						_, projected, err := f.proj.GetModulesPrimary(ctx, f.id)
						require.NoError(t, err)
						require.False(t, projected, "rejected cold loyalty state must remain eligible for fresh hydration")
					}
					require.NoError(t, f.proj.SetUser(ctx, f.id, f.user))
					_, allowed, err := f.store.Capture(ctx, f.id)
					require.NoError(t, err)
					require.False(t, allowed, "old enable/config cannot become admitted after settings expire")
					mods, _, err := f.proj.GetModulesPrimary(ctx, f.id)
					require.NoError(t, err)
					_, stalePresent := mods["loyalty"]
					require.False(t, stalePresent)
					if path == "hydration" {
						require.True(t, mods["timers"].IsEnabled, "loyalty fencing must not constrain unrelated modules")
					}
					require.NoError(t, f.proj.SetModules(ctx, f.id, []projection.ModuleView{current}))
					snap, allowed, err := f.store.Capture(ctx, f.id)
					require.NoError(t, err)
					require.Equal(t, enabled, allowed, "equal canonical revision may restore a lost projection")
					if enabled {
						require.JSONEq(t, string(current.Configs), string(snap.Config))
					}
				})
			}
		}
	}
}

func TestLoyaltyRevisionBootstrapsWarmSettingsBeforeRejectingOldWrite(t *testing.T) {
	for _, path := range []string{"event", "hydration"} {
		t.Run(path, func(t *testing.T) {
			f := newModuleRevisionFixture(t)
			ctx := t.Context()
			current := projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: false, Revision: 2}
			require.NoError(t, f.proj.SetModule(ctx, f.id, current))
			// A warm projection made by an old writer has no persistent fence.
			require.NoError(t, f.client.Do(ctx, f.client.B().Hdel().Key(watchtime.AdmissionKey(f.id)).Field("loyalty_revision").Build()).Error())
			stale := current
			stale.Revision, stale.IsEnabled = 1, true
			writeOld := func() {
				if path == "event" {
					require.NoError(t, f.proj.SetModule(ctx, f.id, stale))
				} else {
					require.NoError(t, f.proj.SetModules(ctx, f.id, []projection.ModuleView{stale}))
				}
			}
			writeOld()
			revision, err := f.client.Do(ctx, f.client.B().Hget().Key(watchtime.AdmissionKey(f.id)).Field("loyalty_revision").Build()).AsInt64()
			require.NoError(t, err)
			require.EqualValues(t, 2, revision)
			f.expire(t)
			writeOld()
			require.NoError(t, f.proj.SetUser(ctx, f.id, f.user))
			_, allowed, err := f.store.Capture(ctx, f.id)
			require.NoError(t, err)
			require.False(t, allowed)
		})
	}
}

func TestLoyaltyRevisionResetsOnlyForNewIncarnation(t *testing.T) {
	f := newModuleRevisionFixture(t)
	ctx := t.Context()
	old := projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: false, Revision: 100}
	require.NoError(t, f.proj.SetModule(ctx, f.id, old))
	deleted, err := f.store.DeleteAccount(ctx, f.id, 100)
	require.NoError(t, err)
	require.True(t, deleted)
	old.IsEnabled = true
	require.NoError(t, f.proj.SetModule(ctx, f.id, old))
	_, allowed, err := f.store.Capture(ctx, f.id)
	require.NoError(t, err)
	require.False(t, allowed, "module replay cannot revive a tombstoned account")
	f.user.AccountCreatedAt = 101
	require.NoError(t, f.proj.SetUser(ctx, f.id, f.user))
	fresh := old
	fresh.AccountCreatedAt, fresh.Revision = 101, 1
	require.NoError(t, f.proj.SetModule(ctx, f.id, fresh))
	require.NoError(t, f.client.Do(ctx, f.client.B().Set().Key("live:"+f.sid).Value("new").Build()).Error())
	require.NoError(t, f.proj.SetModule(ctx, f.id, old))
	require.NoError(t, f.proj.SetModules(ctx, f.id, []projection.ModuleView{old}))
	snap, allowed, err := f.store.Capture(ctx, f.id)
	require.NoError(t, err)
	require.True(t, allowed)
	require.EqualValues(t, 101, snap.AccountCreatedAt)
	revision, err := f.client.Do(ctx, f.client.B().Hget().Key(watchtime.AdmissionKey(f.id)).Field("loyalty_revision").Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, revision, "old account's high revision cannot poison a replacement")
}

func TestLoyaltyRevisionMissingFromOldSnapshotKeepsHydrationIncomplete(t *testing.T) {
	f := newModuleRevisionFixture(t)
	ctx := t.Context()
	current := projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: true, Revision: 2}
	require.NoError(t, f.proj.SetModule(ctx, f.id, current))
	f.expire(t)
	// The old snapshot can predate creation of the loyalty row entirely.
	// Its completeness marker must not hide known missing canonical data.
	require.NoError(t, f.proj.SetModules(ctx, f.id, nil))
	_, projected, err := f.proj.GetModulesPrimary(ctx, f.id)
	require.NoError(t, err)
	require.False(t, projected)
	require.NoError(t, f.proj.SetModules(ctx, f.id, []projection.ModuleView{current}))
	mods, projected, err := f.proj.GetModulesPrimary(ctx, f.id)
	require.NoError(t, err)
	require.True(t, projected)
	require.True(t, mods["loyalty"].IsEnabled)
}

func TestLoyaltyRevisionWarmBootstrapWhenSnapshotOmitsLoyalty(t *testing.T) {
	for _, otherModules := range [][]projection.ModuleView{nil, {{Name: "timers", Revision: 1, IsEnabled: true}}} {
		t.Run("other_module_count="+strconv.Itoa(len(otherModules)), func(t *testing.T) {
			f := newModuleRevisionFixture(t)
			ctx := t.Context()
			current := projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: false, Revision: 2}
			require.NoError(t, f.proj.SetModule(ctx, f.id, current))
			require.NoError(t, f.client.Do(ctx, f.client.B().Hdel().Key(watchtime.AdmissionKey(f.id)).Field("loyalty_revision").Build()).Error())
			// An older full snapshot may omit the row, but the warm stamped
			// projection still supplies a trusted current-incarnation revision.
			require.NoError(t, f.proj.SetModules(ctx, f.id, otherModules))
			revision, err := f.client.Do(ctx, f.client.B().Hget().Key(watchtime.AdmissionKey(f.id)).Field("loyalty_revision").Build()).AsInt64()
			require.NoError(t, err)
			require.EqualValues(t, 2, revision)
			f.expire(t)
			current.Revision, current.IsEnabled = 1, true
			require.NoError(t, f.proj.SetModule(ctx, f.id, current))
			require.NoError(t, f.proj.SetUser(ctx, f.id, f.user))
			_, allowed, err := f.store.Capture(ctx, f.id)
			require.NoError(t, err)
			require.False(t, allowed)
		})
	}
}
