// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeLiveStore struct {
	outcome   projection.LiveOutcome
	applyErr  error
	batches   []projection.LiveBatch
	seededAt  time.Time
	totals    map[uint64]map[projection.CounterName]int64
	boardDone bool
	boards    map[projection.CounterName][]projection.BoardEntry
}

func newFakeLiveStore() *fakeLiveStore {
	return &fakeLiveStore{totals: map[uint64]map[projection.CounterName]int64{}, boards: map[projection.CounterName][]projection.BoardEntry{}}
}

func (f *fakeLiveStore) ApplyLiveCounters(_ context.Context, b projection.LiveBatch) (projection.LiveOutcome, error) {
	f.batches = append(f.batches, b)
	return f.outcome, f.applyErr
}

func (f *fakeLiveStore) SeedLiveCounters(_ context.Context, userID uint64, values []projection.CounterValue, seededAt time.Time) error {
	f.seededAt = seededAt
	f.totals[userID] = map[projection.CounterName]int64{}
	for _, v := range values {
		f.totals[userID][v.Name] = v.Value
	}
	return nil
}

func (f *fakeLiveStore) GetLiveCounters(_ context.Context, userID uint64, names []projection.CounterName) (map[projection.CounterName]int64, bool, error) {
	got, ok := f.totals[userID]
	return got, ok, nil
}

func (f *fakeLiveStore) BoardSeeded(context.Context, projection.CounterName) (bool, error) {
	return f.boardDone, nil
}

func (f *fakeLiveStore) SeedBoard(_ context.Context, name projection.CounterName, entries []projection.BoardEntry) error {
	f.boards[name] = entries
	return nil
}

func (f *fakeLiveStore) DeleteLiveCounters(context.Context, uint64, []projection.CounterName) error {
	return nil
}

func bump(name, scope string, delta int64) data.CounterBumpEntry {
	return data.CounterBumpEntry{Name: name, Scope: scope, Delta: delta}
}

func TestLiveScopeDeltasKeepOnlyTrackedTotals(t *testing.T) {
	viewer := bump(data.CounterMessagesProcessed, data.CounterScopeChannel, 50)
	viewer.ViewerID = 9
	command := bump(data.CounterCommandsAnswered, data.CounterScopeChannel, 50)
	command.Command = "hug"
	cases := []struct {
		name   string
		userID uint64
		bumps  []data.CounterBumpEntry
		want   []projection.CounterValue
	}{
		{
			name:   "bot totals, no boards",
			userID: 0,
			bumps: []data.CounterBumpEntry{
				bump(data.CounterMessagesProcessed, data.CounterScopeBot, 3),
				bump(data.CounterMessagesProcessed, data.CounterScopeBot, 4),
				bump(data.CounterEventsProcessed, data.CounterScopeChannel, 99),
				bump(data.CounterCommandsAnswered, data.CounterScopeBot, 99),
			},
			want: []projection.CounterValue{{Name: data.CounterMessagesProcessed, Value: 7}},
		},
		{
			name:   "channel totals flag the boards",
			userID: 42,
			bumps: []data.CounterBumpEntry{
				bump(data.CounterEventsProcessed, data.CounterScopeChannel, 5),
				bump(data.CounterModActionsTaken, data.CounterScopeChannel, 1),
				bump("points_spent", data.CounterScopeChannel, 8),
				viewer, command,
			},
			want: []projection.CounterValue{
				{Name: data.CounterEventsProcessed, Value: 5, Board: true},
				{Name: data.CounterModActionsTaken, Value: 1},
			},
		},
		{
			name:   "deltas that cancel out are dropped",
			userID: 42,
			bumps: []data.CounterBumpEntry{
				bump(data.CounterMessagesProcessed, data.CounterScopeChannel, 2),
				bump(data.CounterMessagesProcessed, data.CounterScopeChannel, -2),
			},
			want: []projection.CounterValue{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, liveScopeFor(tc.userID).deltas(tc.bumps))
		})
	}
}

func counterPayload(t *testing.T, dto data.CounterBumpedDTO) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(dto)
	require.NoError(t, err)
	return &bus.Message{UUID: "msg-1", Payload: body}
}

func TestHandleCounterBumpsAppliesTrackedDeltas(t *testing.T) {
	live := newFakeLiveStore()
	p := &Projector{live: live, log: zap.NewNop()}
	before := time.Now()
	err := p.HandleCounterBumps(counterPayload(t, data.CounterBumpedDTO{
		UserID: 0, Bumps: []data.CounterBumpEntry{bump(data.CounterEventsProcessed, data.CounterScopeBot, 12)},
	}))
	require.NoError(t, err)
	require.Len(t, live.batches, 1)
	got := live.batches[0]
	require.Equal(t, []any{uint64(0), "msg-1"}, []any{got.UserID, got.MsgID})
	require.False(t, got.StoredAt.Before(before), "a message without a store time is stamped on arrival")
}

func TestHandleCounterBumpsIgnoresBatchesWithNothingTracked(t *testing.T) {
	live := newFakeLiveStore()
	p := &Projector{live: live, log: zap.NewNop()}
	require.NoError(t, p.HandleCounterBumps(counterPayload(t, data.CounterBumpedDTO{
		UserID: 42, Bumps: []data.CounterBumpEntry{bump("deaths", data.CounterScopeChannel, 1)},
	})))
	require.NoError(t, p.HandleCounterBumps(&bus.Message{Payload: []byte("{not json")}))
	require.Empty(t, live.batches)
}

func TestApplyCounterBumpsSeedsOnceTheStoreAsks(t *testing.T) {
	live := newFakeLiveStore()
	live.outcome = projection.LiveNeedsSeed
	seeded := map[projection.CounterName]int64{
		data.CounterMessagesProcessed: 900, data.CounterEventsProcessed: 1500,
		data.CounterCommandsAnswered: 30, data.CounterModActionsTaken: 4,
	}
	loyalty := &fakeLoyaltyReader{ok: true, values: map[string]int64{}}
	for name, v := range seeded {
		loyalty.values[string(name)] = v
	}
	p := &Projector{live: live, loyalty: loyalty, log: zap.NewNop()}
	before := time.Now()

	err := p.applyCounterBumps(context.Background(), "m", time.Now(), data.CounterBumpedDTO{
		UserID: 42, Bumps: []data.CounterBumpEntry{bump(data.CounterMessagesProcessed, data.CounterScopeChannel, 1)},
	})
	require.NoError(t, err)
	require.Equal(t, seeded, live.totals[42])
	require.False(t, live.seededAt.Before(before))
}

func TestApplyCounterBumpsReturnsErrorsForRedelivery(t *testing.T) {
	batch := data.CounterBumpedDTO{UserID: 0, Bumps: []data.CounterBumpEntry{bump(data.CounterMessagesProcessed, data.CounterScopeBot, 1)}}
	applyFail := errors.New("valkey down")
	cases := []struct {
		name    string
		outcome projection.LiveOutcome
		apply   error
		loyalty *fakeLoyaltyReader
		want    error
	}{
		{"store write fails", projection.LiveApplied, applyFail, &fakeLoyaltyReader{ok: true}, applyFail},
		{"seed read fails", projection.LiveNeedsSeed, nil, &fakeLoyaltyReader{ok: false}, errLiveSeedRead},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			live := newFakeLiveStore()
			live.outcome, live.applyErr = tc.outcome, tc.apply
			p := &Projector{live: live, loyalty: tc.loyalty, log: zap.NewNop()}
			require.ErrorIs(t, p.applyCounterBumps(context.Background(), "m", time.Now(), batch), tc.want)
			require.Empty(t, live.totals)
		})
	}
}

func TestLiveTotalsSeedOnlyWhenMissing(t *testing.T) {
	live := newFakeLiveStore()
	live.totals[7] = map[projection.CounterName]int64{data.CounterMessagesProcessed: 11}
	loyalty := &fakeLoyaltyReader{ok: true, values: map[string]int64{data.CounterMessagesProcessed: 5}}
	p := &Projector{live: live, loyalty: loyalty, log: zap.NewNop()}

	got, ok := p.liveTotals(context.Background(), 7, baselineCounters)
	require.True(t, ok)
	require.Equal(t, int64(11), got[data.CounterMessagesProcessed])
	require.Zero(t, loyalty.reads)

	got, ok = p.liveTotals(context.Background(), 8, baselineCounters)
	require.True(t, ok)
	require.Equal(t, int64(5), got[data.CounterMessagesProcessed])
	require.Equal(t, len(channelCounters), loyalty.reads)
}

func TestSeedBoardReadsLoyaltyOnlyOnce(t *testing.T) {
	rows := []loyaltyrpc.CounterRank{{UserID: "1", Value: 90}, {UserID: "junk", Value: 70}, {UserID: "2", Value: 40}}
	cases := []struct {
		name      string
		boardDone bool
		loyaltyOK bool
		want      bool
		wantBoard []projection.BoardEntry
	}{
		{"already seeded", true, true, true, nil},
		{"seeds from the loyalty board", false, true, true, []projection.BoardEntry{{UserID: 1, Value: 90}, {UserID: 2, Value: 40}}},
		{"loyalty unavailable retries later", false, false, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			live := newFakeLiveStore()
			live.boardDone = tc.boardDone
			p := &Projector{live: live, loyalty: &fakeLoyaltyReader{ok: tc.loyaltyOK, rows: rows}, log: zap.NewNop()}
			require.Equal(t, tc.want, p.seedBoard(context.Background(), data.CounterMessagesProcessed))
			require.Equal(t, tc.wantBoard, live.boards[data.CounterMessagesProcessed])
		})
	}
}
