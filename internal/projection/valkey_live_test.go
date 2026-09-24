// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type liveFixture struct {
	t      *testing.T
	ctx    context.Context
	store  *Store
	client valkey.Client
	user   uint64
	board  CounterName
	seeded time.Time
}

func newLiveFixture(t *testing.T) liveFixture {
	t.Helper()
	address := os.Getenv("VALKEY_TEST_ADDR")
	if address == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{address},
		Password:    os.Getenv("VALKEY_TEST_PASSWORD"),
	})
	require.NoError(t, err)
	stamp := uint64(time.Now().UnixNano())
	f := liveFixture{
		t: t, ctx: context.Background(), store: NewStore(client), client: client,
		user: stamp, board: CounterName("b" + strconv.FormatUint(stamp, 10)), seeded: time.UnixMilli(time.Now().UnixMilli()),
	}
	t.Cleanup(func() {
		client.Do(f.ctx, client.B().Del().Key(liveCounterKey(f.user), liveBoardKey(f.board), liveBoardSeedFlag+string(f.board)).Build())
		client.Close()
	})
	return f
}

func (f liveFixture) values(messages int64) []CounterValue {
	return []CounterValue{{Name: "events", Value: messages * 2}, {Name: f.board, Value: messages, Board: true}}
}

func (f liveFixture) seed(messages int64) {
	f.t.Helper()
	require.NoError(f.t, f.store.SeedLiveCounters(f.ctx, f.user, f.values(messages), f.seeded))
}

func (f liveFixture) apply(msgID string, storedAt time.Time, messages int64) LiveOutcome {
	f.t.Helper()
	out, err := f.store.ApplyLiveCounters(f.ctx, LiveBatch{
		UserID: f.user, MsgID: f.member() + msgID, StoredAt: storedAt, Deltas: f.values(messages),
	})
	require.NoError(f.t, err)
	return out
}

func (f liveFixture) member() string { return strconv.FormatUint(f.user, 10) }

func (f liveFixture) totals() (map[CounterName]int64, bool) {
	f.t.Helper()
	got, seeded, err := f.store.GetLiveCounters(f.ctx, f.user, []CounterName{"events", f.board})
	require.NoError(f.t, err)
	return got, seeded
}

func (f liveFixture) boardScore(member string) (float64, bool) {
	f.t.Helper()
	score, err := f.client.Do(f.ctx, f.client.B().Zscore().Key(liveBoardKey(f.board)).Member(member).Build()).AsFloat64()
	if valkey.IsValkeyNil(err) {
		return 0, false
	}
	require.NoError(f.t, err)
	return score, true
}

func TestLiveCountersNeedSeedBeforeApplying(t *testing.T) {
	f := newLiveFixture(t)
	require.Equal(t, LiveNeedsSeed, f.apply("m1", time.Now(), 5))
	_, seeded := f.totals()
	require.False(t, seeded)
	_, onBoard := f.boardScore(f.member())
	require.False(t, onBoard)
}

func TestLiveCountersApplyOnlyBatchesStoredAfterTheSeed(t *testing.T) {
	cases := []struct {
		name     string
		offset   time.Duration
		want     LiveOutcome
		wantMsgs int64
	}{
		{"stored after the seed", time.Second, LiveApplied, 107},
		{"stored at the seed instant", 0, LiveApplied, 107},
		{"stored before the seed", -time.Millisecond, LiveSkipped, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newLiveFixture(t)
			f.seed(100)
			require.Equal(t, tc.want, f.apply("m1", f.seeded.Add(tc.offset), 7))
			got, _ := f.totals()
			require.Equal(t, map[CounterName]int64{"events": tc.wantMsgs * 2, f.board: tc.wantMsgs}, got)
		})
	}
}

func TestLiveCountersCountARedeliveredBatchOnce(t *testing.T) {
	f := newLiveFixture(t)
	f.seed(10)
	later := f.seeded.Add(time.Second)
	require.Equal(t, LiveApplied, f.apply("m1", later, 3))
	require.Equal(t, LiveSkipped, f.apply("m1", later, 3))
	require.Equal(t, LiveApplied, f.apply("m2", later, 4))
	got, _ := f.totals()
	require.Equal(t, int64(17), got[f.board])
}

func TestLiveCountersKeepTheFirstSeed(t *testing.T) {
	f := newLiveFixture(t)
	f.seed(10)
	f.seed(999)
	got, seeded := f.totals()
	require.True(t, seeded)
	require.Equal(t, int64(10), got[f.board])
}

func TestLiveCountersTrackTheBoardAtTheHashTotal(t *testing.T) {
	f := newLiveFixture(t)
	f.seed(40)
	score, _ := f.boardScore(f.member())
	require.Equal(t, float64(40), score)
	f.apply("m1", f.seeded.Add(time.Second), 2)
	score, _ = f.boardScore(f.member())
	require.Equal(t, float64(42), score)
	_, err := f.client.Do(f.ctx, f.client.B().Zscore().Key(liveBoardKey("events")).Member(f.member()).Build()).AsFloat64()
	require.True(t, valkey.IsValkeyNil(err), "a counter without a board never gets a board entry")
}

func TestSeedBoardKeepsFresherTotals(t *testing.T) {
	f := newLiveFixture(t)
	f.seed(500)
	seeded, err := f.store.BoardSeeded(f.ctx, f.board)
	require.NoError(t, err)
	require.False(t, seeded)

	require.NoError(t, f.store.SeedBoard(f.ctx, f.board, []BoardEntry{{UserID: f.user, Value: 3}, {UserID: 1, Value: 9}}))
	t.Cleanup(func() { f.client.Do(f.ctx, f.client.B().Zrem().Key(liveBoardKey(f.board)).Member("1").Build()) })

	mine, _ := f.boardScore(f.member())
	other, _ := f.boardScore("1")
	require.Equal(t, []float64{500, 9}, []float64{mine, other})
	seeded, err = f.store.BoardSeeded(f.ctx, f.board)
	require.NoError(t, err)
	require.True(t, seeded)
}

func TestDeleteLiveCountersClearsHashAndBoard(t *testing.T) {
	f := newLiveFixture(t)
	f.seed(5)
	require.NoError(t, f.store.DeleteLiveCounters(f.ctx, f.user, []CounterName{f.board}))
	_, seeded := f.totals()
	_, onBoard := f.boardScore(f.member())
	require.Equal(t, []bool{false, false}, []bool{seeded, onBoard})
}
