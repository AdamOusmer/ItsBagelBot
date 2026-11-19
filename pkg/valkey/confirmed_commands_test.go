// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey_test

import (
	"strconv"
	"testing"

	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"
	"github.com/stretchr/testify/require"
)

func TestConfirmedCommandsResumePartialExecution(t *testing.T) {
	client := valkeytest.Spawn(t)
	ctx := t.Context()
	require.NoError(t, client.Do(ctx, client.B().Set().Key("blocked").Value("wrong type").Build()).Error())
	batch := pkgvalkey.ConfirmedBatch{ID: "test:receipt", Commands: [][]string{
		{"HINCRBY", "counts", "healthy", "7"},
		{"HINCRBY", "blocked", "recovered", "9"},
		{"SADD", "binary", "\x00\xff\x80"},
	}}
	require.Error(t, pkgvalkey.ConfirmCommands(ctx, client, batch))
	require.NoError(t, client.Do(ctx, client.B().Del().Key("blocked").Build()).Error())
	require.NoError(t, pkgvalkey.ConfirmCommands(ctx, client, batch))
	got, err := client.Do(ctx, client.B().Hgetall().Key("counts").Build()).AsIntMap()
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"healthy": 7}, got)
	got, err = client.Do(ctx, client.B().Hgetall().Key("blocked").Build()).AsIntMap()
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"recovered": 9}, got)
	members, err := client.Do(ctx, client.B().Smembers().Key("binary").Build()).AsStrSlice()
	require.NoError(t, err)
	require.Equal(t, []string{"\x00\xff\x80"}, members)
	receipts, err := client.Do(ctx, client.B().Exists().Key(batch.ID).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, receipts)
}

func TestConfirmedCommandsSplitLargeGenerations(t *testing.T) {
	client := valkeytest.Spawn(t)
	batch := pkgvalkey.ConfirmedBatch{ID: "test:large"}
	for i := range 600 {
		batch.Commands = append(batch.Commands, []string{"HINCRBY", "many", strconv.Itoa(i), "1"})
	}
	require.NoError(t, pkgvalkey.ConfirmCommands(t.Context(), client, batch))
	counts, err := client.Do(t.Context(), client.B().Hgetall().Key("many").Build()).AsIntMap()
	require.NoError(t, err)
	want := make(map[string]int64)
	for i := range 600 {
		want[strconv.Itoa(i)] = 1
	}
	require.Equal(t, want, counts)
}
