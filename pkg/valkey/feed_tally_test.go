// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReceiptTallyAppliesOncePerReceiptAcrossHashesAndRankings(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	unique := testUniqueKey(t)
	tally := ReceiptTally{Receipt: Key{Name: "test:receipt:" + unique, TTL: time.Minute}}
	hashes := []HashTally{
		{Key: "test:tally:session:" + unique, TTL: time.Minute, Fields: []FieldDelta{{"gifts", 5}, {"bits", 100}}},
		{Key: "test:tally:week:" + unique, TTL: time.Minute, Fields: []FieldDelta{{"gifts", 5}}},
	}
	ranked := []RankedDelta{{Key: "test:rank:" + unique, TTL: time.Minute, Member: "42", Delta: 5}}

	first, err := tally.Add(ctx, client, hashes, ranked)
	require.NoError(t, err)
	repeat, err := tally.Add(ctx, client, hashes, ranked)
	require.NoError(t, err)

	require.True(t, first)
	require.False(t, repeat)
	session, err := HashAllInts(ctx, client, hashes[0].Key)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"gifts": 5, "bits": 100}, session)
	top, err := RankedTally{Key: ranked[0].Key, TTL: time.Minute}.Top(ctx, client, 3)
	require.NoError(t, err)
	require.Equal(t, []RankedEntry{{Member: "42", Total: 5}}, top)
}

func TestRankedTallyOrdersHighestFirstAndTopLimits(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	board := RankedTally{Key: "test:rank:" + testUniqueKey(t), TTL: time.Minute}

	for member, delta := range map[string]int64{"a": 3, "b": 9, "c": 5} {
		require.NoError(t, board.Add(ctx, client, member, delta))
	}
	require.NoError(t, board.Add(ctx, client, "a", 10))

	top, err := board.Top(ctx, client, 2)
	require.NoError(t, err)
	all, err := board.All(ctx, client)
	require.NoError(t, err)

	require.Equal(t, []RankedEntry{{"a", 13}, {"b", 9}}, top)
	require.Len(t, all, 3)
}

func TestVersionedHashKeepsTheNewestVersionPerField(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	hash := VersionedHash{Key: "test:vhash:" + testUniqueKey(t), TTL: time.Minute}

	newer, err := hash.Set(ctx, client, "follow", VersionedValue{Value: "b", Version: 20})
	require.NoError(t, err)
	stale, err := hash.Set(ctx, client, "follow", VersionedValue{Value: "a", Version: 10})
	require.NoError(t, err)
	other, err := hash.Set(ctx, client, "sub", VersionedValue{Value: "s", Version: 5})
	require.NoError(t, err)

	got, err := hash.Read(ctx, client)
	require.NoError(t, err)
	require.Equal(t, []bool{true, false, true}, []bool{newer, stale, other})
	require.Equal(t, map[string]VersionedValue{"follow": {"b", 20}, "sub": {"s", 5}}, got)
}

func TestPeriodWindowsUseUTCCalendarBoundaries(t *testing.T) {
	at := time.Date(2026, 12, 31, 23, 30, 0, 0, time.FixedZone("west", -5*3600))
	for _, tc := range []struct {
		period Period
		want   string
	}{
		{PeriodDay, "d20270101"},
		{PeriodWeek, "w202653"},
		{PeriodMonth, "m202701"},
		{PeriodSession, "sabc"},
	} {
		require.Equal(t, tc.want, tc.period.Window(at, "abc"), tc.period)
	}
	require.Equal(t, "w202601", PeriodWeek.Window(time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC), ""))
}

func TestScoredLogReturnsNewestFirstAndReplacesDuplicates(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	log := ScoredLog{Key: "test:scored:" + testUniqueKey(t), TTL: time.Minute}

	for score, member := range map[float64]string{1: "a", 3: "c", 2: "b"} {
		require.NoError(t, log.Add(ctx, client, score, member))
	}
	require.NoError(t, log.Add(ctx, client, 4, "a"))
	newest, err := log.Newest(ctx, client, 2)
	require.NoError(t, err)
	all, err := log.All(ctx, client)
	require.NoError(t, err)

	require.Equal(t, []string{"a", "c"}, newest)
	require.Equal(t, []string{"a", "c", "b"}, all)
}

func TestCASDocumentUpdateAppliesEveryConcurrentChange(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	doc := CASDocument{Key: "test:cas:" + testUniqueKey(t), TTL: time.Minute}
	const writers = 8

	var group sync.WaitGroup
	for range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			assert.NoError(t, doc.Update(ctx, client, func(raw string) (string, error) {
				count, _ := strconv.Atoi(raw)
				return strconv.Itoa(count + 1), nil
			}))
		}()
	}
	group.Wait()
	final, err := doc.Read(ctx, client)

	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(writers), final)
}

func TestVersionedHashReadFieldsSkipsMissingFields(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := t.Context()
	hash := VersionedHash{Key: "test:vhash:" + testUniqueKey(t), TTL: time.Minute}
	_, err := hash.Set(ctx, client, "a", VersionedValue{Value: "x", Version: 5})
	require.NoError(t, err)

	got, err := hash.ReadFields(ctx, client, "a", "missing")

	require.NoError(t, err)
	require.Equal(t, map[string]VersionedValue{"a": {Value: "x", Version: 5}}, got)
}
