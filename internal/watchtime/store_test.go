// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package watchtime_test

import (
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"
	"context"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestPrimaryAdmissionFencesAndOwnedOutbox(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR requires an isolated real Valkey")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	require.NoError(t, err)
	defer client.Close()
	ctx := context.Background()
	id := uint64(time.Now().UnixNano()%100000000 + 8000000000)
	sid := strconv.FormatUint(id, 10)
	keys := []string{"settings:" + sid, watchtime.AdmissionKey(id), "live:" + sid, "watchtime:operations:" + sid, "loyaltick:state:" + sid, "loyaltick:claim:" + sid}
	defer client.Do(ctx, client.B().Del().Key(keys...).Build())
	store := watchtime.NewStore(client)
	proj := projection.NewStore(client)
	_, ok, err := store.Capture(ctx, id)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = store.RestoreAccount(ctx, id, 100)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, proj.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 100, IsActive: true, Status: "paid"}))
	require.NoError(t, proj.SetModule(ctx, id, projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: true, Revision: 1}))
	require.NoError(t, client.Do(ctx, client.B().Set().Key("live:"+sid).Value("s").Build()).Error())
	snap, ok, err := store.Capture(ctx, id)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, proj.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 100, IsActive: true, Status: "paid"}))
	same, ok, err := store.Capture(ctx, id)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, snap.Generation, same.Generation)
	a := data.WatchAwardDTO{UserID: id, AccountCreatedAt: 100, Generation: snap.Generation, LiveSession: snap.LiveSession, WindowID: "w", Entries: []data.LoyaltyEarnEntry{{ViewerID: 77, Points: 10, WatchSeconds: 300}}}
	ok, err = store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, proj.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 100, IsActive: false, Status: "paid"}))
	ok, err = store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, proj.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 100, IsActive: true, Status: "paid"}))
	ok, err = store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.False(t, ok)
	snap, ok, err = store.Capture(ctx, id)
	require.NoError(t, err)
	require.True(t, ok)
	a.Generation = snap.Generation
	require.NoError(t, client.Do(ctx, client.B().Sadd().Key("trial:desired").Member(sid).Build()).Error())
	ok, err = store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, client.Do(ctx, client.B().Srem().Key("trial:desired").Member(sid).Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Hset().Key("loyaltick:state:"+sid).FieldValue().FieldValue("active", "1").FieldValue("session", "stable").FieldValue("window", "w").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Set().Key("loyaltick:claim:"+sid).Value("owner").Build()).Error())
	snap, ok, err = store.Capture(ctx, id)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "stable", snap.LiveSession)
	a.LiveSession = "stable"
	ok, err = store.EnqueueOwned(ctx, a, "wrong")
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = store.EnqueueOwned(ctx, a, "owner")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, client.Do(ctx, client.B().Set().Key("live:"+sid).Value("recheck").Build()).Error())
	ok, err = store.EnqueueOwned(ctx, a, "owner")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = store.DeleteAccount(ctx, id, 100)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = store.RestoreAccount(ctx, id, 100)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = store.RestoreAccount(ctx, id, 101)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = store.DeleteAccount(ctx, id, 100)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, proj.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 101, IsActive: true, Status: "paid"}))
	require.NoError(t, proj.SetModule(ctx, id, projection.ModuleView{AccountCreatedAt: 101, Name: "loyalty", IsEnabled: true, Revision: 1}))
	require.NoError(t, client.Do(ctx, client.B().Set().Key("live:"+sid).Value("new").Build()).Error())
	require.NoError(t, proj.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 100, IsActive: false, Status: "standard"}))
	require.NoError(t, proj.SetModule(ctx, id, projection.ModuleView{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: false, Revision: 999}))
	require.NoError(t, proj.SetModules(ctx, id, []projection.ModuleView{{AccountCreatedAt: 100, Name: "loyalty", IsEnabled: false, Revision: 999}}))
	_, ok, err = store.Capture(ctx, id)
	require.NoError(t, err)
	require.True(t, ok, "old instance writes cannot alter recreated account")
}

func TestAccountStateOrderingSurvivesSettingsExpiry(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR requires isolated real Valkey")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	require.NoError(t, err)
	defer client.Close()
	ctx := t.Context()
	id := uint64(time.Now().UnixNano()%100000000 + 8200000000)
	sid := strconv.FormatUint(id, 10)
	defer client.Do(context.Background(), client.B().Del().Key("settings:"+sid, watchtime.AdmissionKey(id)).Build())
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
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR requires isolated real Valkey")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	require.NoError(t, err)
	defer client.Close()
	ctx := t.Context()
	id := uint64(time.Now().UnixNano()%100000000 + 8300000000)
	sid := strconv.FormatUint(id, 10)
	keys := []string{"settings:" + sid, watchtime.AdmissionKey(id), "live:" + sid, "loyaltick:state:" + sid, "loyaltick:claim:" + sid}
	defer client.Do(context.Background(), client.B().Del().Key(keys...).Build())
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
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR requires isolated real Valkey")
	}
	for _, order := range []string{"user first", "module first", "module event first"} {
		t.Run(order, func(t *testing.T) {
			client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
			require.NoError(t, err)
			defer client.Close()
			ctx := t.Context()
			id := uint64(time.Now().UnixNano()%100000000 + 8400000000)
			sid := strconv.FormatUint(id, 10)
			defer client.Do(context.Background(), client.B().Del().Key("settings:"+sid, watchtime.AdmissionKey(id), "live:"+sid).Build())
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
