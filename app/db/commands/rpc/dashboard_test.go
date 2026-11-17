// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/enttest"
	"ItsBagelBot/app/db/commands/repository"
	commandsrpc "ItsBagelBot/internal/domain/rpc/commands"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	entsql "entgo.io/ent/dialect/sql"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHandleUpsertRestoreCollidingWithLiveCommandAppliesEdits(t *testing.T) {
	client := testdb.Open(t, "commands-dashboard-restore", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewCommands(client, bustest.NewPublisher(), nil, zap.NewNop())
	defer repo.Close(t.Context())

	require.NoError(t, repo.Upsert(1001, repository.CommandSpec{
		Name: "!hello", Response: "live wording", IsActive: true, Perm: "everyone", Cooldown: 5,
	}))
	repo.Close(t.Context())

	repo2 := repository.NewCommands(client, bustest.NewPublisher(), nil, zap.NewNop())
	defer repo2.Close(t.Context())

	d := &dashboardRPC{repo: repo2, log: zap.NewNop()}
	reply, err := d.handleUpsert(t.Context(), commandsrpc.DashboardRequest{
		Name:        "!hello",
		Response:    "edited wording",
		IsActive:    true,
		Perm:        "everyone",
		Cooldown:    9,
		RestoreUses: 42,
	}, 1001)
	require.NoError(t, err)
	require.False(t, reply.Restored)

	row := client.Commands.Query().OnlyX(t.Context())
	require.Equal(t, "edited wording", row.Response)
	require.Equal(t, uint(9), row.Cooldown)
	require.Equal(t, int64(0), row.Uses)
}

func TestDashboardUpsertReturnsCommittedOutcome(t *testing.T) {
	for _, mysql := range []bool{false, true} {
		t.Run(map[bool]string{false: "SQLite", true: "MySQL"}[mysql], func(t *testing.T) {
			client := dashboardStore(t, mysql)
			repo := repository.NewCommands(client, bustest.NewPublisher(), nil, zap.NewNop())
			t.Cleanup(func() { require.NoError(t, repo.Close(context.Background())) })
			client.Commands.Use(rejectBadDashboardCommand)
			d := &dashboardRPC{repo: repo, log: zap.NewNop()}
			for _, name := range []string{"good", "bad"} {
				_, err := d.handleUpsert(t.Context(), commandsrpc.DashboardRequest{
					Name: name, Response: "saved", IsActive: true, Perm: "everyone",
				}, 1001)
				require.Equal(t, name == "bad", err != nil)
			}
			require.Equal(t, "saved", client.Commands.Query().OnlyX(t.Context()).Response)
		})
	}
}

func dashboardStore(t *testing.T, mysql bool) *ent.Client {
	t.Helper()
	if !mysql {
		return testdb.Open(t, "commands-dashboard-durable", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	}
	raw := testdb.MySQL(t, "commands_rpc_durable_")
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	require.NoError(t, client.Schema.Create(t.Context()))
	return client
}

func rejectBadDashboardCommand(next ent.Mutator) ent.Mutator {
	return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
		if name, _ := m.Field("name"); name == "bad" {
			_ = m.SetField("response", "")
		}
		return next.Mutate(ctx, m)
	})
}
