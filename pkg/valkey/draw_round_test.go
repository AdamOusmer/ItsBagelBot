// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"slices"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

const (
	testRoundID   = "round-a"
	testPeerID    = "round-ab"
	testViewer    = "viewer"
	testDrawTTL   = time.Minute
	testOpenPhase = "open"
)

func testDrawKeys(t *testing.T, id string) RoundKeys {
	prefix := "test:draw:" + t.Name() + ":"
	return RoundKeys{
		Joinable: prefix + "joinable", Active: prefix + "active", Due: prefix + "due", Claimable: prefix + "claimable",
		Round: prefix + "round:" + id, Entries: prefix + "entries:" + id, Labels: prefix + "labels:" + id,
		Slots: prefix + "slots:" + id, Lease: prefix + "lease:" + id, Refused: prefix + "refused:" + id,
	}
}

func cleanDrawKeys(t *testing.T, client valkey_go.Client, keys RoundKeys) {
	t.Helper()
	cleanKeys(t, client, keys.Joinable, keys.Active, keys.Due, keys.Claimable, keys.Round, keys.Entries, keys.Labels, keys.Slots, keys.Lease, keys.Refused)
}

func openTestDraw(t *testing.T, client valkey_go.Client, keys RoundKeys) {
	t.Helper()
	code, err := keys.Open(t.Context(), client, RoundOpen{
		RoundID: testRoundID, Fields: []FieldValue{{Field: "phase", Value: testOpenPhase}}, TTL: testDrawTTL,
	})
	require.NoError(t, err)
	require.Equal(t, RoundOpened, code)
}

func TestDrawRoundOpen(t *testing.T) {
	client := valkeytest.Real(t)
	cases := []struct {
		name string
		seed func(*testing.T, RoundKeys)
		want int64
	}{
		{"free", func(*testing.T, RoundKeys) {}, RoundOpened},
		{"live", func(t *testing.T, keys RoundKeys) {
			code, err := keys.Open(t.Context(), client, RoundOpen{
				RoundID: testPeerID, Fields: []FieldValue{{Field: "phase", Value: testOpenPhase}}, TTL: testDrawTTL,
			})
			require.NoError(t, err)
			require.Equal(t, RoundOpened, code)
		}, RoundJoinableBusy},
		{"expired", func(t *testing.T, keys RoundKeys) {
			require.NoError(t, NewKV(client).Set(t.Context(), Key{Name: keys.Joinable, TTL: testDrawTTL}, testPeerID))
		}, RoundOpened},
		{"legacy", func(t *testing.T, keys RoundKeys) {
			require.NoError(t, NewKV(client).Set(t.Context(), Key{Name: keys.Joinable + ":legacy", TTL: testDrawTTL}, "1"))
		}, RoundLegacyBusy},
		{"legacy deadline reserved", func(t *testing.T, keys RoundKeys) {
			require.NoError(t, NewKV(client).Set(t.Context(), Key{Name: keys.Joinable + ":deadline", TTL: testDrawTTL}, "1"))
		}, RoundLegacyBusy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			keys, peer := testDrawKeys(t, testRoundID), testDrawKeys(t, testPeerID)
			cleanDrawKeys(t, client, keys)
			legacy := keys.Joinable + ":legacy"
			deadline := keys.Joinable + ":deadline"
			cleanKeys(t, client, peer.Round, legacy, deadline)
			tc.seed(t, peer)
			code, err := keys.Open(ctx, client, RoundOpen{
				RoundID: testRoundID, Legacy: legacy, LegacyDeadline: deadline, Fields: []FieldValue{{Field: "phase", Value: testOpenPhase}},
				TTL: testDrawTTL, DueAt: -time.Second,
			})
			require.NoError(t, err)
			read, err := keys.Read(ctx, client)
			require.NoError(t, err)
			lapsed, _, err := (DueIndex{Key: keys.Due}).Lapsed(ctx, client, 10)
			require.NoError(t, err)
			type result struct {
				code       int64
				found, due bool
			}
			opened := tc.want == RoundOpened
			require.Equal(t, result{tc.want, opened, opened}, result{code, read.Found, slices.Contains(lapsed, testRoundID)})
		})
	}
}

func TestDrawRoundJoin(t *testing.T) {
	client := valkeytest.Real(t)
	keys := testDrawKeys(t, testRoundID)
	cleanDrawKeys(t, client, keys)
	openTestDraw(t, client, keys)
	cases := []struct {
		name  string
		entry RoundEntrant
		close bool
		want  JoinResult
	}{
		{"first join", RoundEntrant{Member: testViewer, Label: "login", Weight: 120, TTL: testDrawTTL}, false, JoinResult{true, true, 1}},
		{"repeat join", RoundEntrant{Member: testViewer, Label: "renamed", Weight: 200, TTL: testDrawTTL}, false, JoinResult{true, false, 1}},
		{"second viewer", RoundEntrant{Member: "other", Label: "other login", Weight: 100, TTL: testDrawTTL}, false, JoinResult{true, true, 2}},
		{"after close", RoundEntrant{Member: "late", Label: "late login", Weight: 100, TTL: testDrawTTL}, true, JoinResult{false, false, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.close {
				ok, err := keys.Shift(t.Context(), client, PhaseShift{From: testOpenPhase, To: "closing", TTL: testDrawTTL})
				require.NoError(t, err)
				require.True(t, ok)
			}
			got, err := keys.Join(t.Context(), client, tc.entry)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	read, err := keys.Read(t.Context(), client)
	require.NoError(t, err)
	require.Equal(t, RoundRead{
		Round: map[string]string{"phase": "closing"}, Slots: map[string]string{},
		Labels:  map[string]string{testViewer: "renamed", "other": "other login"},
		Entries: []ScoredMember{{Member: "other", Score: 100}, {Member: testViewer, Score: 120}}, Found: true,
	}, read)
}

func TestDrawRoundShift(t *testing.T) {
	client := valkeytest.Real(t)
	keys := testDrawKeys(t, testRoundID)
	cleanDrawKeys(t, client, keys)
	openTestDraw(t, client, keys)
	type snapshot struct {
		applied                              bool
		phase, pointer, action, closes, slot string
	}
	cases := []struct {
		name, owner string
		shift       PhaseShift
		want        snapshot
	}{
		{"wrong phase", testRoundID, PhaseShift{From: "closing", To: "settling", Release: true, Action: `"ignored"`},
			snapshot{false, testOpenPhase, testRoundID, "", "", ""}},
		{"amend", testRoundID, PhaseShift{From: testOpenPhase, To: testOpenPhase, Action: `"extend"`, Fields: []FieldValue{{"closes_at", "42"}}},
			snapshot{true, testOpenPhase, testRoundID, `["extend"]`, "42", ""}},
		{"stale deadline", testRoundID, PhaseShift{From: testOpenPhase, To: "closing", Release: true, Match: FieldValue{"closes_at", "43"}},
			snapshot{false, testOpenPhase, testRoundID, `["extend"]`, "42", ""}},
		{"other owner", testPeerID, PhaseShift{From: testOpenPhase, To: "closing", Release: true, Action: `"close"`},
			snapshot{true, "closing", testPeerID, `["extend","close"]`, "42", ""}},
		{"seed and release", testRoundID, PhaseShift{From: "closing", To: "settling", Release: true, Seed: []FieldValue{{"1", `"original"`}}},
			snapshot{true, "settling", "", `["extend","close"]`, "42", `"original"`}},
		{"preserve seed", testPeerID, PhaseShift{From: "settling", To: "settling", Seed: []FieldValue{{"1", `"replacement"`}}},
			snapshot{true, "settling", testPeerID, `["extend","close"]`, "42", `"original"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			require.NoError(t, NewKV(client).Set(ctx, Key{Name: keys.Joinable, TTL: testDrawTTL}, tc.owner))
			tc.shift.RoundID, tc.shift.TTL = testRoundID, testDrawTTL
			applied, err := keys.Shift(ctx, client, tc.shift)
			require.NoError(t, err)
			read, err := keys.Read(ctx, client)
			require.NoError(t, err)
			pointer, err := NewKV(client).ReadString(ctx, keys.Joinable)
			require.NoError(t, err)
			require.Equal(t, tc.want, snapshot{applied, read.Round["phase"], pointer, read.Round["actions"], read.Round["closes_at"], read.Slots["1"]})
		})
	}
}

func TestDrawRoundRefuse(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	keys := testDrawKeys(t, testRoundID)
	cleanDrawKeys(t, client, keys)
	for _, tc := range []struct {
		viewer, reason string
		first          bool
		count          int64
	}{
		{testViewer, "role", true, 1},
		{testViewer, "points", false, 1},
		{"other", "watch", true, 2},
	} {
		first, count, err := keys.Refuse(ctx, client, Refusal{Viewer: tc.viewer, Reason: tc.reason, TTL: testDrawTTL})
		require.NoError(t, err)
		require.Equal(t, tc.first, first)
		require.Equal(t, tc.count, count)
	}
	fields, err := HashAll(ctx, client, keys.Refused)
	require.NoError(t, err)
	require.Equal(t, map[string]string{testViewer: "role", "other": "watch"}, fields)
	ttl, err := TTLSeconds(ctx, client, keys.Refused)
	require.NoError(t, err)
	require.Greater(t, ttl, int64(0))
}

func TestDrawPhaseRefreshesActiveExpiry(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	keys := testDrawKeys(t, testRoundID)
	cleanDrawKeys(t, client, keys)
	openTestDraw(t, client, keys)
	require.NoError(t, Expire(ctx, client, keys.Active, 2*time.Second))
	got, err := keys.Shift(ctx, client, PhaseShift{From: testOpenPhase, To: "closing", TTL: testDrawTTL})
	require.NoError(t, err)
	require.True(t, got)
	ttl, err := TTLSeconds(ctx, client, keys.Active)
	require.NoError(t, err)
	require.Greater(t, ttl, int64(2))
}

func TestDrawRoundTeardown(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	keys, peer := testDrawKeys(t, testRoundID), testDrawKeys(t, testPeerID)
	cleanDrawKeys(t, client, keys)
	cleanDrawKeys(t, client, peer)
	openTestDraw(t, client, keys)
	_, err := keys.Join(ctx, client, RoundEntrant{Member: testViewer, Label: "login", Weight: 100, TTL: testDrawTTL})
	require.NoError(t, err)
	_, err = keys.Shift(ctx, client, PhaseShift{
		From: testOpenPhase, To: "closing", Release: true, RoundID: testRoundID, TTL: testDrawTTL,
		Seed: []FieldValue{{Field: "1", Value: "pending"}},
	})
	require.NoError(t, err)
	code, err := peer.Open(ctx, client, RoundOpen{
		RoundID: testPeerID, Fields: []FieldValue{{Field: "phase", Value: testOpenPhase}}, TTL: testDrawTTL, OpenedMs: 1,
	})
	require.NoError(t, err)
	require.Equal(t, RoundOpened, code)
	active, err := ActiveRounds(ctx, client, keys.Active)
	require.NoError(t, err)
	require.Equal(t, []string{testPeerID, testRoundID}, active)
	_, _, err = keys.Refuse(ctx, client, Refusal{Viewer: testViewer, Reason: "role", TTL: testDrawTTL})
	require.NoError(t, err)
	require.NoError(t, NewKV(client).Set(ctx, Key{Name: keys.Lease, TTL: testDrawTTL}, "owner"))
	for viewer, refs := range map[string]string{
		testViewer: `["round-a:1:1","round-ab:1:1"]`, "old viewer": `["round-a:2:1"]`,
	} {
		require.NoError(t, (HashField{Key: keys.Claimable, Field: viewer}).Set(ctx, client, refs))
	}
	for _, label := range []string{"initial", "replayed"} {
		t.Run(label, func(t *testing.T) {
			require.NoError(t, keys.Teardown(ctx, client, testRoundID))
			assertDrawTeardown(t, client, keys)
		})
	}
}

func assertDrawTeardown(t *testing.T, client valkey_go.Client, keys RoundKeys) {
	t.Helper()
	ctx := t.Context()
	remaining, err := client.Do(ctx, ExistsCmd(client, keys.Round, keys.Entries, keys.Labels, keys.Slots, keys.Lease, keys.Refused)).AsInt64()
	require.NoError(t, err)
	claimable, err := HashAll(ctx, client, keys.Claimable)
	require.NoError(t, err)
	pointer, err := NewKV(client).ReadString(ctx, keys.Joinable)
	require.NoError(t, err)
	active, err := ActiveRounds(ctx, client, keys.Active)
	require.NoError(t, err)
	due, _, err := (DueIndex{Key: keys.Due}).Lapsed(ctx, client, 10)
	require.NoError(t, err)
	type snapshot struct {
		remaining   int64
		claims      map[string]string
		pointer     string
		active, due []string
	}
	require.Equal(t, snapshot{0, map[string]string{testViewer: `["round-ab:1:1"]`}, testPeerID, []string{testPeerID}, []string{testPeerID}},
		snapshot{remaining, claimable, pointer, active, due})
}

func TestDrawSchedulePreservesNewWake(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	keys := testDrawKeys(t, testRoundID)
	cleanDrawKeys(t, client, keys)
	openTestDraw(t, client, keys)
	applied, err := keys.Shift(ctx, client, PhaseShift{From: testOpenPhase, To: "closing",
		RoundID: testRoundID, TTL: testDrawTTL, Wake: DrawWake{At: time.UnixMilli(0), Channel: "draw:test:wake"}})
	require.NoError(t, err)
	require.True(t, applied)
	index := DueIndex{Key: keys.Due}
	cases := []struct {
		revision string
		want     []string
	}{{"", []string{testRoundID}}, {"1", nil}}
	for _, tc := range cases {
		require.NoError(t, keys.Schedule(ctx, client, DrawSchedule{RoundID: testRoundID,
			Revision: tc.revision, After: time.Hour}))
		due, _, err := index.Lapsed(ctx, client, 10)
		require.NoError(t, err)
		require.ElementsMatch(t, tc.want, due)
	}
}
