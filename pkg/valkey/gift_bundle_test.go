// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestGiftBundleCollectsRecipientsAndTakesOnce(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	key := "test:gift:{" + testUniqueKey(t) + "}"
	bundle := GiftBundle{TTL: time.Minute, Window: 5 * time.Second}

	opened, err := bundle.Open(ctx, client, key, GiftHeader{ID: "gift-1", Total: 2, Payload: []byte("header")})
	require.NoError(t, err)
	repeat, err := bundle.Open(ctx, client, key, GiftHeader{ID: "gift-1", Total: 2, Payload: []byte("header")})
	require.NoError(t, err)
	first, err := bundle.Add(ctx, client, key, []byte("a"))
	require.NoError(t, err)
	second, err := bundle.Add(ctx, client, key, []byte("b"))
	require.NoError(t, err)
	taken, ok, err := bundle.Take(ctx, client, key, "gift-1")
	require.NoError(t, err)
	_, again, err := bundle.Take(ctx, client, key, "gift-1")
	require.NoError(t, err)

	require.Equal(t, GiftWaiting, opened)
	require.Equal(t, GiftAlreadyOpen, repeat)
	require.Equal(t, GiftAdded{BundleID: "gift-1"}, first)
	require.Equal(t, GiftAdded{BundleID: "gift-1", Complete: true}, second)
	require.True(t, ok)
	require.Equal(t, GiftTaken{Header: []byte("header"), Recipients: [][]byte{[]byte("a"), []byte("b")}}, taken)
	require.False(t, again)
}

func TestGiftBundleAdoptsRecipientsParkedBeforeTheHeader(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	key := "test:gift:{" + testUniqueKey(t) + "}"
	bundle := GiftBundle{TTL: time.Minute, Window: 5 * time.Second}

	parked, err := bundle.Add(ctx, client, key, []byte("early"))
	require.NoError(t, err)
	opened, err := bundle.Open(ctx, client, key, GiftHeader{ID: "gift-2", Total: 1, Payload: []byte("header")})
	require.NoError(t, err)
	taken, ok, err := bundle.Take(ctx, client, key, "gift-2")
	require.NoError(t, err)

	require.Equal(t, GiftAdded{}, parked)
	require.Equal(t, GiftComplete, opened)
	require.True(t, ok)
	require.Equal(t, [][]byte{[]byte("early")}, taken.Recipients)
}

func TestGiftBundleFillsTheOldestOpenBundleFirst(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	key := "test:gift:{" + testUniqueKey(t) + "}"
	bundle := GiftBundle{TTL: time.Minute, Window: 5 * time.Second}

	_, err := bundle.Open(ctx, client, key, GiftHeader{ID: "old", Total: 1, Payload: []byte("old")})
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	_, err = bundle.Open(ctx, client, key, GiftHeader{ID: "new", Total: 1, Payload: []byte("new")})
	require.NoError(t, err)
	first, err := bundle.Add(ctx, client, key, []byte("a"))
	require.NoError(t, err)
	second, err := bundle.Add(ctx, client, key, []byte("b"))
	require.NoError(t, err)

	require.Equal(t, "old", first.BundleID)
	require.Equal(t, "new", second.BundleID)
}
