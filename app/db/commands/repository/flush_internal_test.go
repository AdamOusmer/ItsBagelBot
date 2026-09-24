// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/commands/ent/enttest"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus/bustest"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

func TestUpsertEachIsolatesPoisonItem(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("commandsflush"))
	t.Cleanup(func() { _ = client.Close() })

	r := NewCommands(client, bustest.NewPublisher(), nil, zap.NewNop())

	ctx := context.Background()
	good := data.CommandChangedDTO{UserID: 1001, Name: "ok", Response: "fine", IsActive: true, Perm: "everyone"}
	poison := data.CommandChangedDTO{UserID: 1001, Name: "bad", Response: "", IsActive: true, Perm: "everyone"}

	landed := r.upsertEach(ctx, nil, []data.CommandChangedDTO{good, poison})

	require.Len(t, landed, 1)
	assert.Equal(t, "ok", landed[0].Name)

	rows := client.Commands.Query().AllX(ctx)
	require.Len(t, rows, 1)
	assert.Equal(t, "ok", rows[0].Name)

	r.Close(ctx)
	assert.Len(t, client.Commands.Query().AllX(ctx), 1)
}

func TestBulkUpsertPreservesUses(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("commandsuses"))
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	client.Commands.Create().
		SetUserID(1001).
		SetName("hello").
		SetResponse("old wording").
		SetUses(42).
		SaveX(ctx)

	edit := data.CommandChangedDTO{UserID: 1001, Name: "hello", Response: "new wording", IsActive: true, Perm: "everyone"}
	require.NoError(t, bulkUpsertCommands(ctx, client, []data.CommandChangedDTO{edit}))

	row := client.Commands.Query().OnlyX(ctx)
	assert.Equal(t, "new wording", row.Response)
	assert.Equal(t, int64(42), row.Uses)
}

func TestRecordUsePreservesProducerBatchIncrements(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("commandsusesgroup"))
	t.Cleanup(func() { _ = client.Close() })

	r := NewCommands(client, bustest.NewPublisher(), nil, zap.NewNop())

	ctx := context.Background()
	for _, name := range []string{"a", "b", "c"} {
		client.Commands.Create().
			SetUserID(1001).
			SetName(name).
			SetResponse("r").
			SetUses(10).
			SaveX(ctx)
	}

	pend := map[commandKey]int64{
		{userID: 1001, name: "a"}:       1,
		{userID: 1001, name: "b"}:       1,
		{userID: 1001, name: "c"}:       3,
		{userID: 1001, name: "deleted"}: 1,
	}
	for key, count := range pend {
		require.NoError(t, r.RecordUse(ctx, "batch-"+key.name, data.CommandUsedDTO{UserID: key.userID, Name: key.name, Count: count}))
	}
	defer r.Close(ctx)

	rows := client.Commands.Query().AllX(ctx)
	got := map[string]int64{}
	for _, row := range rows {
		got[row.Name] = row.Uses
	}
	assert.Equal(t, map[string]int64{"a": 11, "b": 11, "c": 13}, got)
}
