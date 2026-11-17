// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"database/sql"
	"path/filepath"
	"testing"

	commandsent "ItsBagelBot/app/db/commands/ent"
	_ "ItsBagelBot/app/db/commands/ent/runtime"
	commandsrepo "ItsBagelBot/app/db/commands/repository"
	loyaltyent "ItsBagelBot/app/db/loyalty/ent"
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type completionReceipts struct {
	loyalty  *loyaltyrepo.Loyalty
	commands *commandsrepo.Commands
	rows     *commandsent.Client
}

func completionSQL(t *testing.T, dialect, name string) *sql.DB {
	t.Helper()
	if dialect == "mysql" {
		return testdb.MySQL(t, "completion_"+name+"_")
	}
	raw, err := sql.Open(testdb.Driver, filepath.Join(t.TempDir(), name+".db")+"?_fk=1")
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	return raw
}

func newCompletionReceipts(t *testing.T, dialect string) completionReceipts {
	t.Helper()
	loyaltyDriver := entsql.OpenDB(dialect, completionSQL(t, dialect, "loyalty"))
	loyaltyClient := loyaltyent.NewClient(loyaltyent.Driver(loyaltyDriver))
	require.NoError(t, loyaltyClient.Schema.Create(t.Context()))
	loyalty := loyaltyrepo.NewLoyalty(loyaltyClient, loyaltyDriver, nil, zap.NewNop())
	require.NoError(t, loyalty.EnsureWatchSchema(t.Context()))
	t.Cleanup(func() { loyalty.Close(t.Context()) })
	commandClient := commandsent.NewClient(commandsent.Driver(entsql.OpenDB(dialect, completionSQL(t, dialect, "commands"))))
	require.NoError(t, commandClient.Schema.Create(t.Context()))
	_, err := commandClient.Commands.Create().SetUserID(123).SetName("hello").SetResponse("hello").Save(t.Context())
	require.NoError(t, err)
	commands := commandsrepo.NewCommands(commandClient, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { require.NoError(t, commands.Close(t.Context())) })
	return completionReceipts{loyalty: loyalty, commands: commands, rows: commandClient}
}

func (v completionReceipts) replay(t *testing.T, js nats.JetStreamContext) {
	t.Helper()
	info, err := js.StreamInfo(bus.DataStream.Name)
	require.NoError(t, err)
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq; seq++ {
		msg, err := js.GetMsg(bus.DataStream.Name, seq)
		require.NoError(t, err)
		for range 2 {
			v.apply(t, msg)
		}
	}
	balance, found, err := v.loyalty.BalanceGet(t.Context(), 123, 999)
	require.NoError(t, err)
	require.True(t, found)
	row, err := v.rows.Commands.Query().Only(t.Context())
	require.NoError(t, err)
	require.Equal(t, []int64{10, 1}, []int64{balance.Points, row.Uses})
}

func (v completionReceipts) apply(t *testing.T, msg *nats.RawStreamMsg) {
	t.Helper()
	switch msg.Subject {
	case data.SubjectLoyaltyEarned:
		var dto data.LoyaltyEarnedDTO
		require.NoError(t, codec.Unmarshal(msg.Data, &dto))
		require.NoError(t, v.loyalty.ApplyEarned(t.Context(), dto))
	case data.SubjectCommandUsed:
		var dto data.CommandUsedDTO
		require.NoError(t, codec.Unmarshal(msg.Data, &dto))
		require.NoError(t, v.commands.RecordUse(t.Context(), dto.BatchID, dto))
	}
}
