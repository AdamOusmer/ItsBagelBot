// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestRetainIfPresentOnlyRescoresAnExistingKey(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	index := "test:retain:index:" + t.Name()
	retain := RetainIfPresent{Key: "test:retain:missing:" + t.Name(), Index: index}

	require.NoError(t, retain.Run(ctx, client, "ghost", time.Minute, 1234))
	score, err := client.Do(ctx, client.B().Zscore().Key(index).Member("ghost").Build()).ToFloat64()
	require.Error(t, err, "a missing key must not be indexed")

	present := RetainIfPresent{Key: "test:retain:present:" + t.Name(), Index: index}
	require.NoError(t, client.Do(ctx, client.B().Set().Key(present.Key).Value("1").Build()).Error())
	require.NoError(t, present.Run(ctx, client, "real", time.Minute, 5678))
	score, err = client.Do(ctx, client.B().Zscore().Key(index).Member("real").Build()).ToFloat64()
	require.NoError(t, err)
	require.Equal(t, float64(5678), score)
}

func TestRetentionSweepDropsGoneKeysAndKeepsLiveOnes(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	index := "test:sweep:index:" + t.Name()
	setIdx := "test:sweep:set:" + t.Name()
	keyPrefix := "test:sweep:item:" + t.Name() + ":"
	now := time.Now()

	require.NoError(t, client.Do(ctx, client.B().Set().Key(keyPrefix+"live").Value("1").Ex(time.Hour).Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Zadd().Key(index).ScoreMember().
		ScoreMember(float64(now.Add(-time.Minute).Unix()), "gone").
		ScoreMember(float64(now.Add(-time.Minute).Unix()), "live").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Sadd().Key(setIdx).Member("gone", "live").Build()).Error())

	sweep := RetentionSweep{Index: index, SetIdx: []string{setIdx}, KeyPrefix: keyPrefix}
	_, err := sweep.Run(ctx, client, now, 500, time.Minute)
	require.NoError(t, err)

	goneScore, err := client.Do(ctx, client.B().Zscore().Key(index).Member("gone").Build()).ToFloat64()
	require.Error(t, err, "the gone member must be dropped from the index")
	_ = goneScore
	inSet, err := client.Do(ctx, client.B().Sismember().Key(setIdx).Member("gone").Build()).AsBool()
	require.NoError(t, err)
	require.False(t, inSet)

	liveScore, err := client.Do(ctx, client.B().Zscore().Key(index).Member("live").Build()).ToFloat64()
	require.NoError(t, err)
	require.Greater(t, liveScore, float64(now.Unix()))
}

func TestRetentionMigrationCarriesLiveMembersAndDrainsTheSource(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	source := "test:migrate:source:" + t.Name()
	index := "test:migrate:index:" + t.Name()
	cursor := "test:migrate:cursor:" + t.Name()
	keyPrefix := "test:migrate:item:" + t.Name() + ":"

	require.NoError(t, client.Do(ctx, client.B().Set().Key(keyPrefix+"live").Value("1").Ex(time.Hour).Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Sadd().Key(source).Member("live", "gone").Build()).Error())

	migration := RetentionMigration{Source: source, Index: index, Cursor: cursor}
	n, err := migration.Run(ctx, client, 500, keyPrefix, time.Minute, time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(2), n)

	remaining, err := client.Do(ctx, client.B().Scard().Key(source).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, remaining)

	liveScore, err := client.Do(ctx, client.B().Zscore().Key(index).Member("live").Build()).ToFloat64()
	require.NoError(t, err)
	require.Greater(t, liveScore, float64(0))

	_, err = client.Do(ctx, client.B().Zscore().Key(index).Member("gone").Build()).ToFloat64()
	require.Error(t, err, "a member whose key is gone must not be carried over")
}

func TestGuardedEraseDeletesOnlyWhileTheFieldStillMatches(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	key := "test:erase:" + t.Name()
	sortedIdx := "test:erase:sorted:" + t.Name()
	setIdx := "test:erase:set:" + t.Name()
	erase := GuardedErase{Key: key, SortedIdx: sortedIdx, SetIdx: []string{setIdx}}

	require.NoError(t, client.Do(ctx, client.B().Hset().Key(key).FieldValue().FieldValue("stamp", "100").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Zadd().Key(sortedIdx).ScoreMember().ScoreMember(1, "member").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Sadd().Key(setIdx).Member("member").Build()).Error())

	applied, err := erase.Run(ctx, client, "stamp", "999", "member")
	require.NoError(t, err)
	require.False(t, applied, "a mismatched stamp must not erase")

	applied, err = erase.Run(ctx, client, "stamp", "100", "member")
	require.NoError(t, err)
	require.True(t, applied)

	exists, err := client.Do(ctx, client.B().Exists().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, exists)

	inSorted, err := client.Do(ctx, client.B().Zscore().Key(sortedIdx).Member("member").Build()).ToFloat64()
	require.Error(t, err)
	_ = inSorted
	inSet, err := client.Do(ctx, client.B().Sismember().Key(setIdx).Member("member").Build()).AsBool()
	require.NoError(t, err)
	require.False(t, inSet)
}
