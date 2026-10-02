// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type primaryAdmissionFixture struct {
	client valkey.Client
	store  *watchtime.Store
	proj   *projection.Store
	id     uint64
	sid    string
}

func newPrimaryAdmissionFixture(t *testing.T) *primaryAdmissionFixture {
	t.Helper()
	client := realClient(t, 0)
	id, sid := uniqueID(8000000000)
	cleanupKeys(t, client, "settings:"+sid, watchtime.AdmissionKey(id), "live:"+sid, "watchtime:operations:"+sid, "loyaltick:state:"+sid, "loyaltick:claim:"+sid)
	t.Cleanup(func() { client.Do(context.Background(), client.B().Srem().Key("trial:desired").Member(sid).Build()) })
	return &primaryAdmissionFixture{client: client, store: watchtime.NewStore(client), proj: projection.NewStore(client), id: id, sid: sid}
}

func (f *primaryAdmissionFixture) enable(ctx context.Context, t *testing.T) {
	t.Helper()
	allowed, err := f.store.RestoreAccount(ctx, f.id, 100)
	require.NoError(t, err)
	require.True(t, allowed)
	require.NoError(t, f.proj.SetUser(ctx, f.id, projection.UserProjection{AccountCreatedAt: 100, IsActive: true, Status: "paid"}))
	require.NoError(t, f.proj.SetModule(ctx, f.id, projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: true, Revision: 1}))
	require.NoError(t, f.client.Do(ctx, f.client.B().Set().Key("live:"+f.sid).Value("s").Build()).Error())
}

func awardFor(f *primaryAdmissionFixture, snap watchtime.Snapshot) data.WatchAwardDTO {
	return data.WatchAwardDTO{UserID: f.id, AccountCreatedAt: 100, Generation: snap.Generation, LiveSession: snap.LiveSession, WindowID: "w", Entries: []data.LoyaltyEarnEntry{{ViewerID: 77, Points: 10, WatchSeconds: 300}}}
}

func TestPrimaryAdmissionFencesAndOwnedOutbox(t *testing.T) {
	f := newPrimaryAdmissionFixture(t)
	ctx := t.Context()
	_, ok, err := f.store.Capture(ctx, f.id)
	require.NoError(t, err)
	require.False(t, ok)
	f.enable(ctx, t)
	snap, ok, err := f.store.Capture(ctx, f.id)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, f.proj.SetUser(ctx, f.id, projection.UserProjection{AccountCreatedAt: 100, IsActive: true, Status: "paid"}))
	same, ok, err := f.store.Capture(ctx, f.id)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, snap.Generation, same.Generation)
	a := awardFor(f, snap)
	ok, err = f.store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = f.store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, f.proj.SetUser(ctx, f.id, projection.UserProjection{AccountCreatedAt: 100, IsActive: false, Status: "paid"}))
	ok, err = f.store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, f.proj.SetUser(ctx, f.id, projection.UserProjection{AccountCreatedAt: 100, IsActive: true, Status: "paid"}))
	ok, err = f.store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestOwnedOutboxRequiresEligibleTenantAndCurrentLease(t *testing.T) {
	f := newPrimaryAdmissionFixture(t)
	ctx := t.Context()
	f.enable(ctx, t)
	snap, ok, err := f.store.Capture(ctx, f.id)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, f.client.Do(ctx, f.client.B().Sadd().Key("trial:desired").Member(f.sid).Build()).Error())
	ok, err = f.store.Enqueue(ctx, awardFor(f, snap))
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, f.client.Do(ctx, f.client.B().Srem().Key("trial:desired").Member(f.sid).Build()).Error())
	require.NoError(t, f.client.Do(ctx, f.client.B().Hset().Key("loyaltick:state:"+f.sid).FieldValue().FieldValue("active", "1").FieldValue("session", "stable").FieldValue("window", "w").Build()).Error())
	require.NoError(t, f.client.Do(ctx, f.client.B().Set().Key("loyaltick:claim:"+f.sid).Value("owner").Build()).Error())
	snap, ok, err = f.store.Capture(ctx, f.id)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "stable", snap.LiveSession)
	a := awardFor(f, snap)
	ok, err = f.store.EnqueueOwned(ctx, a, "wrong")
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = f.store.EnqueueOwned(ctx, a, "owner")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, f.client.Do(ctx, f.client.B().Set().Key("live:"+f.sid).Value("recheck").Build()).Error())
	ok, err = f.store.EnqueueOwned(ctx, a, "owner")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAccountIncarnationFencesOutdatedWork(t *testing.T) {
	f := newPrimaryAdmissionFixture(t)
	ctx := t.Context()
	f.enable(ctx, t)
	ok, err := f.store.DeleteAccount(ctx, f.id, 100)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = f.store.RestoreAccount(ctx, f.id, 100)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = f.store.RestoreAccount(ctx, f.id, 101)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = f.store.DeleteAccount(ctx, f.id, 100)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, f.proj.SetUser(ctx, f.id, projection.UserProjection{AccountCreatedAt: 101, IsActive: true, Status: "paid"}))
	require.NoError(t, f.proj.SetModule(ctx, f.id, projection.ModuleView{AccountCreatedAt: 101, Name: "loyalty", IsEnabled: true, Revision: 1}))
	require.NoError(t, f.client.Do(ctx, f.client.B().Set().Key("live:"+f.sid).Value("new").Build()).Error())
	require.NoError(t, f.proj.SetUser(ctx, f.id, projection.UserProjection{AccountCreatedAt: 100, IsActive: false, Status: "standard"}))
	require.NoError(t, f.proj.SetModule(ctx, f.id, projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: false, Revision: 999}))
	require.NoError(t, f.proj.SetModules(ctx, f.id, []projection.ModuleView{{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: false, Revision: 999}}))
	_, ok, err = f.store.Capture(ctx, f.id)
	require.NoError(t, err)
	require.True(t, ok, "old instance writes cannot alter recreated account")
}

func TestAccountStateOrderingSurvivesSettingsExpiry(t *testing.T) {
	client := realClient(t, 0)
	ctx := t.Context()
	id, sid := uniqueID(8200000000)
	cleanupKeys(t, client, "settings:"+sid, watchtime.AdmissionKey(id))
	store := watchtime.NewStore(client)
	proj := projection.NewStore(client)
	allowed, err := store.RestoreAccount(ctx, id, 100)
	require.NoError(t, err)
	require.True(t, allowed)
	old := projection.UserProjection{AccountCreatedAt: 100, StateRevision: 1, IsActive: true, Status: "paid"}
	require.NoError(t, proj.SetUser(ctx, id, old))
	paused := old
	paused.StateRevision = 2
	paused.IsActive = false
	require.NoError(t, proj.SetUser(ctx, id, paused))
	require.NoError(t, proj.SetUser(ctx, id, old))
	_, active, banned, _, _, err := proj.GetUser(ctx, id)
	require.NoError(t, err)
	require.False(t, active)
	require.False(t, banned)
	// The permanent admission metadata must reject old hydration even after
	// the projected settings hash expires. DEL models that loss exactly.
	require.NoError(t, client.Do(ctx, client.B().Del().Key("settings:"+sid).Build()).Error())
	require.NoError(t, proj.SetUser(ctx, id, old))
	count, err := client.Do(ctx, client.B().Exists().Key("settings:"+sid).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, proj.SetUser(ctx, id, paused))
	bannedState := paused
	bannedState.StateRevision = 3
	bannedState.IsActive = true
	bannedState.Banned = true
	require.NoError(t, proj.SetUser(ctx, id, bannedState))
	require.NoError(t, proj.SetUser(ctx, id, paused))
	_, active, banned, _, _, err = proj.GetUser(ctx, id)
	require.NoError(t, err)
	require.True(t, active)
	require.True(t, banned)
	unknown := old
	unknown.StateRevision = 0
	require.NoError(t, proj.SetUser(ctx, id, unknown))
	_, _, banned, _, _, err = proj.GetUser(ctx, id)
	require.NoError(t, err)
	require.True(t, banned)
	deleted, err := proj.DeleteLegacyAccount(ctx, id)
	require.NoError(t, err)
	require.False(t, deleted, "callers must skip live-state cleanup for ignored deletes")
	_, _, banned, _, _, err = proj.GetUser(ctx, id)
	require.NoError(t, err)
	require.True(t, banned, "unknown delete cannot remove known account")
	// Account recreation legitimately starts its revision sequence over.
	allowed, err = store.RestoreAccount(ctx, id, 101)
	require.NoError(t, err)
	require.True(t, allowed)
	old.AccountCreatedAt = 101
	require.NoError(t, proj.SetUser(ctx, id, old))
	_, active, banned, _, _, err = proj.GetUser(ctx, id)
	require.NoError(t, err)
	require.True(t, active)
	require.False(t, banned)
}

func TestAccountHydrationRestoresCanonicalIncarnation(t *testing.T) {
	client := realClient(t, 0)
	ctx := t.Context()
	id, sid := uniqueID(8300000000)
	cleanupKeys(t, client, "settings:"+sid, watchtime.AdmissionKey(id), "live:"+sid, "loyaltick:state:"+sid, "loyaltick:claim:"+sid)
	store := watchtime.NewStore(client)
	proj := projection.NewStore(client)
	user := projection.UserProjection{AccountCreatedAt: 100, StateRevision: 3, IsActive: true, Status: "paid"}
	// No UserChanged event arrives first: a canonical hydration reply alone
	// must establish admission, using the same restoration semantics as events.
	require.NoError(t, proj.SetUserWithTTL(ctx, id, user, time.Minute))
	require.NoError(t, proj.SetModule(ctx, id, projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: true, Revision: 1}))
	require.NoError(t, client.Do(ctx, client.B().Set().Key("live:"+sid).Value("s").Build()).Error())
	snap, allowed, err := store.Capture(ctx, id)
	require.NoError(t, err)
	require.True(t, allowed)
	require.EqualValues(t, 100, snap.AccountCreatedAt)
	deleted, err := store.DeleteAccount(ctx, id, 100)
	require.NoError(t, err)
	require.True(t, deleted)
	require.NoError(t, proj.SetUser(ctx, id, user))
	_, allowed, err = store.Capture(ctx, id)
	require.NoError(t, err)
	require.False(t, allowed, "hydration cannot revive a deleted incarnation")
	user.AccountCreatedAt, user.StateRevision = 101, 1
	require.NoError(t, proj.SetUser(ctx, id, user))
	user.AccountCreatedAt, user.StateRevision = 100, 999
	require.NoError(t, proj.SetUser(ctx, id, user))
	instance, err := client.Do(ctx, client.B().Hget().Key(watchtime.AdmissionKey(id)).Field("instance").Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 101, instance, "old hydration cannot reset a recreated account")
}

func TestAccountHydrationPreservesEitherSectionWriteOrder(t *testing.T) {
	for _, order := range []string{"user first", "module first", "module event first"} {
		t.Run(order, func(t *testing.T) {
			client := realClient(t, 0)
			ctx := t.Context()
			id, sid := uniqueID(8400000000)
			cleanupKeys(t, client, "settings:"+sid, watchtime.AdmissionKey(id), "live:"+sid)
			proj := projection.NewStore(client)
			mod := projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: true, Revision: 1}
			writeUser := func() {
				require.NoError(t, proj.SetUserWithTTL(ctx, id, projection.UserProjection{AccountCreatedAt: 100, StateRevision: 1, IsActive: true, Status: "paid"}, time.Minute))
			}
			writeModule := func() {
				if order == "module event first" {
					require.NoError(t, proj.SetModule(ctx, id, mod))
				} else {
					require.NoError(t, proj.SetModulesWithTTL(ctx, id, []projection.ModuleView{mod}, time.Minute))
				}
			}
			if order == "user first" {
				writeUser()
				writeModule()
			} else {
				writeModule()
				writeUser()
			}
			require.NoError(t, client.Do(ctx, client.B().Set().Key("live:"+sid).Value("s").Build()).Error())
			_, allowed, err := watchtime.NewStore(client).Capture(ctx, id)
			require.NoError(t, err)
			require.True(t, allowed, "either canonical section may finish first without clearing the other")
			mods, _, err := proj.GetModulesPrimary(ctx, id)
			require.NoError(t, err)
			require.True(t, mods["loyalty"].IsEnabled)
		})
	}
}
