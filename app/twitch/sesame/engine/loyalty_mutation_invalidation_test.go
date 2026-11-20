// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalBalanceMutationsInvalidateEveryTopLimit(t *testing.T) {
	for _, verb := range []string{"add", "spend", "transfer", "wager"} {
		t.Run(verb, func(t *testing.T) {
			nc := testnats.Connect(t)
			served := bustest.Respond(t, nc, counterRPCPrefix+".top.get", `{"top":[{"viewer_id":"7","points":5}]}`)
			bustest.Respond(t, nc, counterRPCPrefix+".balance."+verb, `{"found":true,"spent":true,"balance":{"viewer_id":"7","points":4},"target_balance":{"viewer_id":"8","points":1}}`)
			store := NewValkeyLoyaltyStore(newHotPathTestClient(t), NewLoyaltyRPC(nc, counterRPCPrefix), nil, nil)
			t.Cleanup(store.Close)
			channel, other := freshChannel(), freshChannel()
			for _, limit := range []int{5, 10} {
				_, err := store.Top(t.Context(), channel, limit)
				require.NoError(t, err)
			}
			_, err := store.Top(t.Context(), other, 10)
			require.NoError(t, err)
			require.NoError(t, mutateLoyaltyStore(t.Context(), store, channel, verb))
			for _, limit := range []int{5, 10} {
				_, err = store.Top(t.Context(), channel, limit)
				require.NoError(t, err)
			}
			_, err = store.Top(t.Context(), other, 10)
			require.NoError(t, err)
			assert.EqualValues(t, 5, served.Load())
		})
	}
}

func TestBalanceInvalidationConvergesPeerTopCaches(t *testing.T) {
	nc := testnats.Connect(t)
	client := newHotPathTestClient(t)
	channel := freshChannel()
	served := bustest.Respond(t, nc, counterRPCPrefix+".top.get", `{"top":[]}`)
	stores := make([]*ValkeyLoyaltyStore, 2)
	for i := range stores {
		stores[i] = NewValkeyLoyaltyStore(client, NewLoyaltyRPC(nc, counterRPCPrefix), nil, nil)
		stores[i].BindInvalidations(bus.NewInvalidations(nc, invalidationTestPrefix, nil))
		t.Cleanup(stores[i].Close)
		_, err := stores[i].Top(t.Context(), channel, 10)
		require.NoError(t, err)
	}
	require.NoError(t, nc.Flush())
	require.NoError(t, bus.NewInvalidations(nc, invalidationTestPrefix, nil).Publish(invalidate.ScopeLoyalty, channel, "7"))
	assert.Eventually(t, func() bool {
		for _, store := range stores {
			if _, cached := store.top.Get(strconv.FormatUint(channel, 10) + ":10"); cached {
				return false
			}
		}
		return true
	}, time.Second, time.Millisecond)
	for _, store := range stores {
		_, err := store.Top(t.Context(), channel, 10)
		require.NoError(t, err)
	}
	require.EqualValues(t, 4, served.Load())
}

func mutateLoyaltyStore(ctx context.Context, store *ValkeyLoyaltyStore, channel uint64, verb string) error {
	switch verb {
	case "add":
		_, _, err := store.BalanceAdjustViewer(ctx, BalanceAdjustment{BroadcasterID: channel, ViewerID: 7, ViewerLogin: "viewer", Value: 1})
		return err
	case "spend":
		_, _, _, err := store.BalanceSpend(ctx, PointSpend{BroadcasterID: channel, ViewerID: 7, Amount: 1})
		return err
	case "transfer":
		_, _, _, err := store.BalanceTransfer(ctx, PointTransfer{BroadcasterID: channel, FromViewerID: 7, TargetViewerID: 8, TargetLogin: "other", Amount: 1})
		return err
	default:
		_, err := store.BalanceWager(ctx, PointWager{BroadcasterID: channel, ViewerID: 7, Amount: 1})
		return err
	}
}

func TestCounterInvalidationEvictsEveryReplicaWithoutDroppingBalances(t *testing.T) {
	nc := testnats.Connect(t)
	served := bustest.Respond(t, nc, counterRPCPrefix+".counter.get", `{"found":true,"counter":{"name":"hugs","scope":"viewer","value":"3"}}`)
	client := newHotPathTestClient(t)
	channel := freshChannel()
	require.NoError(t, client.Do(t.Context(), client.B().Set().Key(balanceKey(channel, 7)).Value("5:0").ExSeconds(60).Build()).Error())
	stores := make([]*ValkeyLoyaltyStore, 2)
	for i := range stores {
		stores[i] = NewValkeyLoyaltyStore(client, NewLoyaltyRPC(nc, counterRPCPrefix), nil, nil)
		stores[i].BindInvalidations(bus.NewInvalidations(nc, invalidationTestPrefix, nil))
		t.Cleanup(stores[i].Close)
		_, _, err := stores[i].CounterPeek(t.Context(), CounterTarget{BroadcasterID: channel, ViewerID: 7, Name: "hugs"})
		require.NoError(t, err)
	}
	require.NoError(t, nc.Flush())
	require.NoError(t, bus.NewInvalidations(nc, invalidationTestPrefix, nil).Publish(invalidate.ScopeLoyaltyCounters, channel, "hugs"))
	assert.Eventually(t, func() bool {
		for _, store := range stores {
			if _, ok := store.scopes.Get(counterRef{channel, "hugs"}.scopeKey()); ok {
				return false
			}
		}
		return true
	}, time.Second, time.Millisecond)
	assert.False(t, keyGone(t, client, balanceKey(channel, 7)))
	for _, store := range stores {
		_, _, err := store.CounterPeek(t.Context(), CounterTarget{BroadcasterID: channel, ViewerID: 7, Name: "hugs"})
		require.NoError(t, err)
	}
	assert.EqualValues(t, 8, served.Load())
}

func TestLoyaltyReconnectClearsBothLocalCaches(t *testing.T) {
	store := NewValkeyLoyaltyStore(nil, nil, nil, nil)
	t.Cleanup(store.Close)
	feed := &loyaltyGapFeed{}
	store.BindInvalidations(feed)
	store.scopes.Set("scope:17:hugs", "viewer")
	store.top.Set("17:10", nil)
	for _, fn := range feed.gaps {
		fn()
	}
	_, scopeCached := store.scopes.Get("scope:17:hugs")
	_, topCached := store.top.Get("17:10")
	assert.False(t, scopeCached)
	assert.False(t, topCached)
}

func TestAccountCounterInvalidationClearsOnlyChannelViews(t *testing.T) {
	client := newHotPathTestClient(t)
	store := NewValkeyLoyaltyStore(client, nil, nil, nil)
	t.Cleanup(store.Close)
	h := bus.NewInvalidations(nil, "", nil)
	store.BindInvalidations(h)
	channel, other := freshChannel(), freshChannel()
	ref, otherRef := counterRef{channel, "hugs"}, counterRef{other, "hugs"}
	for _, key := range []string{ref.channelKey(), ref.viewerKey(), otherRef.channelKey(), balanceKey(channel, 7)} {
		require.NoError(t, client.Do(t.Context(), client.B().Set().Key(key).Value("5").ExSeconds(60).Build()).Error())
	}
	store.scopes.Set(ref.scopeKey(), "viewer")
	store.scopes.Set(otherRef.scopeKey(), "viewer")
	require.NoError(t, h.Publish(invalidate.ScopeLoyaltyCounters, channel))
	assert.Equal(t, []bool{false, false, false, false}, []bool{
		keyGone(t, client, ref.channelKey()), keyGone(t, client, ref.viewerKey()),
		keyGone(t, client, otherRef.channelKey()), keyGone(t, client, balanceKey(channel, 7)),
	})
	_, cached := store.scopes.Get(ref.scopeKey())
	assert.False(t, cached)
	_, cached = store.scopes.Get(otherRef.scopeKey())
	assert.True(t, cached)
}

type loyaltyGapFeed struct{ gaps []func() }

func (*loyaltyGapFeed) Watch(string, func(cache.Event)) {}

func (f *loyaltyGapFeed) WatchGap(fn func()) { f.gaps = append(f.gaps, fn) }
