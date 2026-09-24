// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"math"
	"os"
	"strconv"
	"strings"
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
		client.Do(f.ctx, client.B().Del().Key(liveCounterKey(f.user), liveBoardKey(f.board), liveBoardSeedFlag+string(f.board), liveBoardMemberPrefix+string(f.board)).Build())
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

func (f liveFixture) boardScore(user string) (int64, bool) {
	f.t.Helper()
	member, err := f.client.Do(f.ctx, f.client.B().Hget().Key(liveBoardMemberPrefix+string(f.board)).Field(user).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return 0, false
	}
	require.NoError(f.t, err)
	score, err := f.client.Do(f.ctx, f.client.B().Zscore().Key(liveBoardKey(f.board)).Member(member).Build()).AsInt64()
	require.NoError(f.t, err)
	require.Zero(f.t, score)
	value, err := strconv.ParseInt(strings.SplitN(member, ":", 2)[0], 10, 64)
	require.NoError(f.t, err)
	return value, true
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
	require.Equal(t, int64(40), score)
	f.apply("m1", f.seeded.Add(time.Second), 2)
	score, _ = f.boardScore(f.member())
	require.Equal(t, int64(42), score)
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
	require.Equal(t, []int64{500, 9}, []int64{mine, other})
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

func TestLiveCountersPreserveFullInt64AndExactRanking(t *testing.T) {
	f := newLiveFixture(t)
	max := int64(math.MaxInt64)
	require.NoError(t, f.store.SeedLiveCounters(f.ctx, f.user, []CounterValue{{Name: f.board, Value: max - 1, Board: true}}, f.seeded))
	require.NoError(t, f.store.SeedBoard(f.ctx, f.board, []BoardEntry{{UserID: 1, Value: max - 2}, {UserID: 2, Value: max}}))
	members, err := f.client.Do(f.ctx, f.client.B().Zrange().Key(liveBoardKey(f.board)).Min("0").Max("-1").Rev().Build()).AsStrSlice()
	require.NoError(t, err)
	require.Equal(t, []string{"9223372036854775807:2", "9223372036854775806:" + f.member(), "9223372036854775805:1"}, members)
	batch := LiveBatch{UserID: f.user, MsgID: f.member() + "max", StoredAt: f.seeded.Add(time.Second), Deltas: []CounterValue{{Name: f.board, Value: 1, Board: true}}}
	outcome, err := f.store.ApplyLiveCounters(f.ctx, batch)
	require.NoError(t, err)
	require.Equal(t, LiveApplied, outcome)
	score, _ := f.boardScore(f.member())
	require.Equal(t, max, score)
	totals, _ := f.totals()
	require.Equal(t, max, totals[f.board])
	batch.MsgID += "overflow"
	_, err = f.store.ApplyLiveCounters(f.ctx, batch)
	require.Error(t, err)
	batch.Deltas[0].Value = -1
	outcome, err = f.store.ApplyLiveCounters(f.ctx, batch)
	require.NoError(t, err, "rejected overflow must not consume the batch receipt")
	require.Equal(t, LiveApplied, outcome)
	score, _ = f.boardScore(f.member())
	require.Equal(t, max-1, score)
}

func TestLiveCounterRejectsUnderflowWithoutPartialUpdate(t *testing.T) {
	f := newLiveFixture(t)
	f.seed(1)
	_, err := f.store.ApplyLiveCounters(f.ctx, LiveBatch{UserID: f.user, MsgID: f.member() + "underflow", StoredAt: f.seeded.Add(time.Second), Deltas: []CounterValue{{Name: "events", Value: 2}, {Name: f.board, Value: -2, Board: true}}})
	require.Error(t, err)
	totals, _ := f.totals()
	require.Equal(t, int64(2), totals["events"])
	require.Equal(t, int64(1), totals[f.board])
}

func TestLiveCountersApplyFullInt64Delta(t *testing.T) {
	f := newLiveFixture(t)
	require.NoError(t, f.store.SeedLiveCounters(f.ctx, f.user, []CounterValue{{Name: f.board, Value: 0, Board: true}}, f.seeded))
	for i, delta := range []int64{math.MaxInt64, -math.MaxInt64} {
		outcome, err := f.store.ApplyLiveCounters(f.ctx, LiveBatch{UserID: f.user, MsgID: f.member() + "full-delta-" + strconv.Itoa(i), StoredAt: f.seeded.Add(time.Second), Deltas: []CounterValue{{Name: f.board, Value: delta, Board: true}}})
		require.NoError(t, err)
		require.Equal(t, LiveApplied, outcome)
		value, _ := f.boardScore(f.member())
		if i == 0 {
			require.Equal(t, int64(math.MaxInt64), value)
		} else {
			require.Zero(t, value)
		}
	}
}

func TestSeedBoardRepairsEvictedBoardFromFresherIndex(t *testing.T) {
	f := newLiveFixture(t)
	f.seed(500)
	require.NoError(t, f.store.SeedBoard(f.ctx, f.board, []BoardEntry{{UserID: f.user, Value: 3}}))
	require.NoError(t, f.client.Do(f.ctx, f.client.B().Del().Key(liveBoardKey(f.board)).Build()).Error())
	seeded, err := f.store.BoardSeeded(f.ctx, f.board)
	require.NoError(t, err)
	require.False(t, seeded, "an evicted board needs seeding even when the seed flag survives")
	require.NoError(t, f.store.SeedBoard(f.ctx, f.board, []BoardEntry{{UserID: f.user, Value: 3}}))
	value, present := f.boardScore(f.member())
	require.True(t, present)
	require.Equal(t, int64(500), value, "repair must preserve the fresher indexed value")
}

func TestDeleteLiveCountersValidatesAllBoardsBeforeDeleting(t *testing.T) {
	f := newLiveFixture(t)
	f.seed(5)
	second := CounterName(string(f.board) + "broken")
	index := liveBoardMemberPrefix + string(second)
	require.NoError(t, f.client.Do(f.ctx, f.client.B().Set().Key(index).Value("wrong-type").Build()).Error())
	t.Cleanup(func() { f.client.Do(f.ctx, f.client.B().Del().Key(index).Build()) })
	require.Error(t, f.store.DeleteLiveCounters(f.ctx, f.user, []CounterName{f.board, second}))
	_, seeded := f.totals()
	value, present := f.boardScore(f.member())
	require.True(t, seeded)
	require.True(t, present)
	require.Equal(t, int64(5), value, "failure on a later board must not remove an earlier one")
	require.NoError(t, f.client.Do(f.ctx, f.client.B().Del().Key(index).Build()).Error())
	require.NoError(t, f.store.DeleteLiveCounters(f.ctx, f.user, []CounterName{f.board, second}))
	_, seeded = f.totals()
	_, present = f.boardScore(f.member())
	require.False(t, seeded)
	require.False(t, present)
}

func TestLiveCounterWrongBoardTypeDoesNotConsumeReceiptOrPartiallyApply(t *testing.T) {
	for _, kind := range []string{"board", "member index"} {
		t.Run(kind, func(t *testing.T) {
			f := newLiveFixture(t)
			f.seed(5)
			badBoard := CounterName(string(f.board) + "wrong")
			badKey := liveBoardKey(badBoard)
			if kind == "member index" {
				badKey = liveBoardMemberPrefix + string(badBoard)
			}
			require.NoError(t, f.client.Do(f.ctx, f.client.B().Set().Key(badKey).Value("wrong-type").Build()).Error())
			t.Cleanup(func() {
				f.client.Do(f.ctx, f.client.B().Del().Key(badKey, liveBoardKey(badBoard), liveBoardMemberPrefix+string(badBoard)).Build())
			})
			batch := LiveBatch{UserID: f.user, MsgID: f.member() + "wrong-type", StoredAt: f.seeded.Add(time.Second), Deltas: []CounterValue{{Name: f.board, Value: 1, Board: true}, {Name: badBoard, Value: 1, Board: true}}}
			_, err := f.store.ApplyLiveCounters(f.ctx, batch)
			require.Error(t, err)
			totals, _ := f.totals()
			require.Equal(t, int64(5), totals[f.board])
			value, _ := f.boardScore(f.member())
			require.Equal(t, int64(5), value)
			receipt, err := f.client.Do(f.ctx, f.client.B().Exists().Key(liveSeenPrefix+batch.MsgID).Build()).AsInt64()
			require.NoError(t, err)
			require.Zero(t, receipt)
			require.NoError(t, f.client.Do(f.ctx, f.client.B().Del().Key(badKey).Build()).Error())
			outcome, err := f.store.ApplyLiveCounters(f.ctx, batch)
			require.NoError(t, err)
			require.Equal(t, LiveApplied, outcome)
			totals, _ = f.totals()
			require.Equal(t, int64(6), totals[f.board])
		})
	}
}

func TestLiveSeedWrongBoardTypeLeavesHashUnseeded(t *testing.T) {
	f := newLiveFixture(t)
	index := liveBoardMemberPrefix + string(f.board)
	require.NoError(t, f.client.Do(f.ctx, f.client.B().Set().Key(index).Value("wrong-type").Build()).Error())
	require.Error(t, f.store.SeedLiveCounters(f.ctx, f.user, f.values(5), f.seeded))
	exists, err := f.client.Do(f.ctx, f.client.B().Exists().Key(liveCounterKey(f.user)).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, exists)
	require.NoError(t, f.client.Do(f.ctx, f.client.B().Del().Key(index).Build()).Error())
	f.seed(5)
}
