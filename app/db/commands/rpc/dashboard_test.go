// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/enttest"
	"ItsBagelBot/app/db/commands/repository"
	commandsrpc "ItsBagelBot/internal/domain/rpc/commands"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"

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
	repo2.Close(t.Context())

	row := client.Commands.Query().OnlyX(t.Context())
	require.Equal(t, "edited wording", row.Response)
	require.Equal(t, uint(9), row.Cooldown)
	require.Equal(t, int64(0), row.Uses)
}
