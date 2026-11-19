// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"encoding/json"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestDrawShiftSlots(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	keys := testDrawKeys(t, testRoundID)
	cleanDrawKeys(t, client, keys)
	openTestDraw(t, client, keys)
	const pending = `{"state":"pending","generation":1}`
	const won = `{"state":"won","generation":1}`
	const rerolled = `{"state":"pending","generation":2}`
	const ref = "round-a:1:1"
	ok, err := keys.Shift(ctx, client, PhaseShift{
		From: testOpenPhase, To: "settling", TTL: testDrawTTL, Seed: []FieldValue{{Field: "1", Value: pending}},
	})
	require.NoError(t, err)
	require.True(t, ok)

	type result struct {
		applied []bool
		slot    string
		refs    []string
		expires bool
	}
	cases := []struct {
		name   string
		shifts []SlotShift
		want   result
	}{
		{"stale batch", []SlotShift{
			{Phase: "settling", Expect: SlotStamp{State: "pending", Generation: 0}, Next: won, Index: ClaimAdd},
			{Phase: "closing", Expect: SlotStamp{State: "pending", Generation: 1}, Next: won, Index: ClaimAdd},
		}, result{[]bool{false, false}, pending, nil, false}},
		{"add claim ref", []SlotShift{
			{Phase: "settling", Expect: SlotStamp{State: "pending", Generation: 1}, Next: won, Index: ClaimAdd},
		}, result{[]bool{true}, won, []string{ref}, true}},
		{"drop claim ref", []SlotShift{
			{Phase: "settling", Expect: SlotStamp{State: "won", Generation: 1}, Next: rerolled, Index: ClaimDrop},
		}, result{[]bool{true}, rerolled, nil, false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.shifts {
				tc.shifts[i].Slot, tc.shifts[i].Viewer, tc.shifts[i].Ref = "1", testViewer, ref
			}
			applied, err := keys.ShiftSlots(ctx, client, tc.shifts)
			require.NoError(t, err)
			read, err := keys.Read(ctx, client)
			require.NoError(t, err)
			refs, err := ReadClaimRefs(ctx, client, keys.Claimable, testViewer)
			require.NoError(t, err)
			ttl, err := TTLSeconds(ctx, client, keys.Claimable)
			require.NoError(t, err)
			require.Equal(t, tc.want, result{applied, read.Slots["1"], refs, ttl > 0})
		})
	}
}

type drawClaimTestSlot struct {
	State      string `json:"state"`
	Generation int64  `json:"generation"`
	Deadline   int64  `json:"deadline"`
	ClaimedAt  int64  `json:"claimed_at,omitempty"`
}

func TestDrawClaimSlots(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	keys, peer := testDrawKeys(t, testRoundID), testDrawKeys(t, testPeerID)
	cleanDrawKeys(t, client, keys)
	cleanKeys(t, client, peer.Slots)
	now, err := ServerNow(ctx, client)
	require.NoError(t, err)
	future := now.Add(testDrawTTL).UnixMilli()
	passed := now.Add(-time.Second).UnixMilli()
	for field, slot := range map[HashField]drawClaimTestSlot{
		{Key: keys.Slots, Field: "1"}: {State: "won", Generation: 1, Deadline: future},
		{Key: peer.Slots, Field: "1"}: {State: "won", Generation: 1, Deadline: future},
		{Key: keys.Slots, Field: "2"}: {State: "won", Generation: 1, Deadline: passed},
		{Key: keys.Slots, Field: "3"}: {State: "won", Generation: 2, Deadline: future},
	} {
		raw, err := json.Marshal(slot)
		require.NoError(t, err)
		require.NoError(t, field.Set(ctx, client, string(raw)))
	}
	const firstRef, peerRef, lateRef, staleRef = "round-a:1:1", "round-ab:1:1", "round-a:2:1", "round-a:3:1"
	claimField := HashField{Key: keys.Claimable, Field: testViewer}
	indexed, err := json.Marshal([]string{firstRef, peerRef, lateRef, staleRef})
	require.NoError(t, err)
	require.NoError(t, claimField.Set(ctx, client, string(indexed)))
	refs := []ClaimRef{
		{Slots: keys.Slots, Ref: firstRef, Slot: "1", Generation: 1},
		{Slots: peer.Slots, Ref: peerRef, Slot: "1", Generation: 1},
		{Slots: keys.Slots, Ref: lateRef, Slot: "2", Generation: 1},
		{Slots: keys.Slots, Ref: staleRef, Slot: "3", Generation: 1},
	}
	type result struct {
		codes     []int64
		remaining []string
	}
	cases := []struct {
		name, viewer string
		want         result
	}{
		{"unindexed viewer", "other viewer", result{[]int64{SlotClaimGone, SlotClaimGone, SlotClaimGone, SlotClaimGone}, []string{firstRef, peerRef, lateRef, staleRef}}},
		{"claim once", testViewer, result{[]int64{SlotClaimed, SlotClaimed, SlotClaimLate, SlotClaimGone}, []string{lateRef, staleRef}}},
		{"replay", testViewer, result{[]int64{SlotClaimGone, SlotClaimGone, SlotClaimLate, SlotClaimGone}, []string{lateRef, staleRef}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codes, err := ClaimSlots(ctx, client, SlotClaim{Claimable: keys.Claimable, Viewer: tc.viewer, Refs: refs, Now: now.Add(time.Hour)})
			require.NoError(t, err)
			remaining, err := ReadClaimRefs(ctx, client, keys.Claimable, testViewer)
			require.NoError(t, err)
			require.Equal(t, tc.want, result{codes, remaining})
		})
	}
	for _, round := range []RoundKeys{keys, peer} {
		read, err := round.Read(ctx, client)
		require.NoError(t, err)
		var claimed drawClaimTestSlot
		require.NoError(t, json.Unmarshal([]byte(read.Slots["1"]), &claimed))
		require.True(t, claimed.ClaimedAt >= now.UnixMilli() && claimed.ClaimedAt < future)
		require.Equal(t, drawClaimTestSlot{State: "claimed", Generation: 1, Deadline: future, ClaimedAt: claimed.ClaimedAt}, claimed)
	}
}

func TestDrawShiftSlotBatchIsAtomic(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	keys := testDrawKeys(t, testRoundID)
	cleanDrawKeys(t, client, keys)
	openTestDraw(t, client, keys)
	const won = `{"state":"won","generation":1}`
	const expired = `{"state":"no_show","generation":1}`
	applied, err := keys.Shift(ctx, client, PhaseShift{
		From: testOpenPhase, To: "settling", TTL: testDrawTTL,
		Seed: []FieldValue{{Field: "1", Value: won}, {Field: "2", Value: won}},
	})
	require.NoError(t, err)
	require.True(t, applied)
	for _, tc := range []struct {
		prior, stored, revision string
		applied                 bool
	}{{expired, won, "", false}, {won, won, "7", false}, {won, expired, "", true}} {
		batch := SlotBatch{Revision: &tc.revision, RoundID: testRoundID, Phase: "settling", Shifts: []SlotShift{
			{Slot: "1", Prior: won, Next: expired},
			{Slot: "2", Prior: tc.prior, Next: expired},
		}}
		applied, err := keys.ShiftSlotBatch(ctx, client, batch)
		require.NoError(t, err)
		read, err := keys.View(ctx, client)
		require.NoError(t, err)
		require.Equal(t, tc.applied, applied)
		require.Equal(t, map[string]string{"1": tc.stored, "2": tc.stored}, read.Slots)
	}
}
