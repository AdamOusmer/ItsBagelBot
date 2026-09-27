// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestModuleActionsReturnCommittedRows(t *testing.T) {
	client := testdb.Open(t, "module-dashboard-toggle", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewModules(client, bustest.NewPublisher(), nil, zap.NewNop())
	defer repo.Close(t.Context())
	d := &dashboardRPC{repo: repo, log: zap.NewNop()}
	created, err := d.handleUpsert(t.Context(), modulesrpc.DashboardRequest{Name: "queue", IsEnabled: true, Configs: codec.RawMessage(`{"option":"off"}`)}, 1001)
	require.NoError(t, err)
	require.Len(t, created.Modules, 1)
	require.Equal(t, 1, created.Modules[0].Revision)
	require.True(t, client.Modules.Query().OnlyX(t.Context()).IsEnabled)
	expected := 1
	patched, err := d.handlePatch(t.Context(), modulesrpc.DashboardRequest{Name: "queue", IsEnabled: true, Configs: codec.RawMessage(`{"option":"on"}`), ExpectedRev: &expected}, 1001)
	require.NoError(t, err)
	require.False(t, patched.Conflict)
	require.Len(t, patched.Modules, 1)
	require.Equal(t, 2, patched.Modules[0].Revision)
	require.Equal(t, patched.Rev, patched.Modules[0].Revision)
	require.JSONEq(t, `{"option":"on","__rev":2}`, string(patched.Modules[0].Configs))
	conflict, err := d.handlePatch(t.Context(), modulesrpc.DashboardRequest{Name: "queue", ExpectedRev: &expected}, 1001)
	require.NoError(t, err)
	require.True(t, conflict.Conflict)
	require.Empty(t, conflict.Modules)
}
