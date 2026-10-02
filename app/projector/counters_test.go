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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func appliedDeltas(live *fakeLiveStore) [][]projection.CounterValue {
	var got [][]projection.CounterValue
	for _, batch := range live.batches {
		got = append(got, batch.Deltas)
	}
	return got
}

func TestCounterBumpsApplyOnlyTrackedTotals(t *testing.T) {
	viewer := bump(data.CounterMessagesProcessed, data.CounterScopeChannel, 50)
	viewer.ViewerID = 9
	command := bump(data.CounterCommandsAnswered, data.CounterScopeChannel, 50)
	command.Command = "hug"

	for _, tc := range []struct {
		name   string
		userID uint64
		bumps  []data.CounterBumpEntry
		want   [][]projection.CounterValue
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
			want: [][]projection.CounterValue{{{Name: data.CounterMessagesProcessed, Value: 7}}},
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
			want: [][]projection.CounterValue{{
				{Name: data.CounterEventsProcessed, Value: 5, Board: true},
				{Name: data.CounterModActionsTaken, Value: 1},
			}},
		},
		{
			name:   "deltas that cancel out are dropped",
			userID: 42,
			bumps: []data.CounterBumpEntry{
				bump(data.CounterMessagesProcessed, data.CounterScopeChannel, 2),
				bump(data.CounterMessagesProcessed, data.CounterScopeChannel, -2),
			},
		},
		{
			name:   "a batch with nothing tracked is ignored",
			userID: 42,
			bumps:  []data.CounterBumpEntry{bump("deaths", data.CounterScopeChannel, 1)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := newFakeLiveStore()
			p := NewProjector(Deps{Live: live, Log: zap.NewNop()})

			require.NoError(t, p.HandleCounterBumps(message(t, "m", data.CounterBumpedDTO{UserID: tc.userID, Bumps: tc.bumps})))

			assert.Equal(t, tc.want, appliedDeltas(live))
		})
	}
}

func TestCounterBumpsCarryMessageIdentityAndArrivalStamp(t *testing.T) {
	live := newFakeLiveStore()
	p := NewProjector(Deps{Live: live, Log: zap.NewNop()})
	before := time.Now()

	require.NoError(t, p.HandleCounterBumps(message(t, "msg-1", data.CounterBumpedDTO{
		Bumps: []data.CounterBumpEntry{bump(data.CounterEventsProcessed, data.CounterScopeBot, 12)},
	})))
	require.NoError(t, p.HandleCounterBumps(&bus.Message{Payload: []byte("{not json")}))

	require.Len(t, live.batches, 1, "an undecodable payload is dropped, not applied")
	assert.Equal(t, []any{uint64(0), "msg-1"}, []any{live.batches[0].UserID, live.batches[0].MsgID})
	assert.False(t, live.batches[0].StoredAt.Before(before), "a message without a store time is stamped on arrival")
}

func TestCounterBumpsSeedFromLoyaltyWhenTheStoreAsks(t *testing.T) {
	seeded := map[string]int64{
		data.CounterMessagesProcessed: 900, data.CounterEventsProcessed: 1500,
		data.CounterCommandsAnswered: 30, data.CounterModActionsTaken: 4,
	}
	applyFailed := errors.New("valkey down")

	for _, tc := range []struct {
		name     string
		mode     loyaltyMode
		outcome  projection.LiveOutcome
		applyErr error
		wantErr  error
		want     map[projection.CounterName]int64
	}{
		{
			name: "seeds every tracked counter once the store asks", mode: loyaltyServes, outcome: projection.LiveNeedsSeed,
			want: map[projection.CounterName]int64{
				data.CounterMessagesProcessed: 900, data.CounterEventsProcessed: 1500,
				data.CounterCommandsAnswered: 30, data.CounterModActionsTaken: 4,
			},
		},
		{name: "does not read loyalty when the batch applied", mode: loyaltyServes, outcome: projection.LiveApplied},
		{name: "a failed store write is returned for redelivery", mode: loyaltyServes, applyErr: applyFailed, wantErr: applyFailed},
		{name: "a refusing loyalty service fails the seed", mode: loyaltyRefuses, outcome: projection.LiveNeedsSeed, wantErr: errLiveSeedRead},
		{name: "a malformed loyalty reply fails the seed", mode: loyaltyMalformed, outcome: projection.LiveNeedsSeed, wantErr: errLiveSeedRead},
		{name: "an unreachable loyalty service fails the seed", mode: loyaltyAbsent, outcome: projection.LiveNeedsSeed, wantErr: errLiveSeedRead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := newFakeLiveStore()
			live.outcome, live.applyErr = tc.outcome, tc.applyErr
			_, loyalty := newLoyalty(t, tc.mode, seeded, nil)
			p := NewProjector(Deps{Live: live, Loyalty: loyalty, Log: zap.NewNop()})
			before := time.Now()

			err := p.HandleCounterBumps(message(t, "m", data.CounterBumpedDTO{
				UserID: 42, Bumps: []data.CounterBumpEntry{bump(data.CounterMessagesProcessed, data.CounterScopeChannel, 1)},
			}))

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, live.totals[42])
			assert.Equal(t, tc.want != nil, !live.seededAt.Before(before) && !live.seededAt.IsZero(), "the seed is stamped after the loyalty reads")
		})
	}
}

func TestCounterBumpsWithoutALoyaltyClientCannotSeed(t *testing.T) {
	live := newFakeLiveStore()
	live.outcome = projection.LiveNeedsSeed
	p := NewProjector(Deps{Live: live, Log: zap.NewNop()})

	err := p.HandleCounterBumps(message(t, "m", data.CounterBumpedDTO{
		UserID: 42, Bumps: []data.CounterBumpEntry{bump(data.CounterMessagesProcessed, data.CounterScopeChannel, 1)},
	}))

	require.ErrorIs(t, err, errLiveSeedRead)
	assert.Empty(t, live.totals)
}

func TestSeedBoardsReadsLoyaltyOnlyOnce(t *testing.T) {
	rows := []loyaltyrpc.CounterRank{{UserID: "1", Value: 90}, {UserID: "junk", Value: 70}, {UserID: "2", Value: 40}}
	seeded := []projection.BoardEntry{{UserID: 1, Value: 90}, {UserID: 2, Value: 40}}

	for _, tc := range []struct {
		name      string
		boardDone bool
		mode      loyaltyMode
		wantBoard []projection.BoardEntry
		wantReads int32
	}{
		{name: "an already seeded board is not read again", boardDone: true, mode: loyaltyServes},
		{name: "seeds from the loyalty board and skips unparseable ids", mode: loyaltyServes, wantBoard: seeded, wantReads: 2},
		{name: "an unavailable loyalty service seeds nothing and retries later", mode: loyaltyAbsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := newFakeLiveStore()
			live.boardDone = tc.boardDone
			service, loyalty := newLoyalty(t, tc.mode, nil, rows)
			p := NewProjector(Deps{Live: live, Loyalty: loyalty, Log: zap.NewNop()})
			ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
			defer cancel()

			p.SeedBoards(ctx)

			assert.Equal(t, tc.wantBoard, live.boards[data.CounterMessagesProcessed])
			assert.Equal(t, tc.wantReads, service.reads.Load())
		})
	}
}
