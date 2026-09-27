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
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPatchExistingRequiresBothGuardsBeforeRepositoryAccess(t *testing.T) {
	id, rev, invalidID, invalidRev := 1, 0, 0, -1
	for _, tc := range []struct {
		name    string
		id, rev *int
	}{
		{"missing both", nil, nil}, {"missing ID", nil, &rev}, {"missing revision", &id, nil}, {"invalid ID", &invalidID, &rev}, {"invalid revision", &id, &invalidRev},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &dashboardRPC{}
			reply, err := d.handlePatchExisting(t.Context(), modulesrpc.DashboardRequest{Name: "queue", ExpectedID: tc.id, ExpectedRev: tc.rev}, 1001)
			require.NoError(t, err)
			require.NotEmpty(t, reply.Error)
			require.Empty(t, reply.Modules)
		})
	}
}

func TestPatchExistingReturnsGuardedCommittedRows(t *testing.T) {
	client := testdb.Open(t, "module-dashboard-existing", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewModules(client, bustest.NewPublisher(), nil, zap.NewNop())
	defer repo.Close(t.Context())
	row := client.Modules.Create().SetUserID(1001).SetName("queue").SetIsEnabled(false).SetConfigs([]byte(`{"option":"off"}`)).SetRevision(0).SaveX(t.Context())
	d := &dashboardRPC{repo: repo, log: zap.NewNop()}
	req := modulesrpc.DashboardRequest{Name: row.Name, IsEnabled: row.IsEnabled, Configs: codec.RawMessage(`{"option":"on"}`), ExpectedRev: &row.Revision, ExpectedID: &row.ID}
	reply, err := d.handlePatchExisting(t.Context(), req, row.UserID)
	require.NoError(t, err)
	require.Empty(t, reply.Error)
	require.False(t, reply.Conflict)
	require.Equal(t, 1, reply.Rev)
	require.Len(t, reply.Modules, 1)
	require.Equal(t, reply.Rev, reply.Modules[0].Revision)
	require.False(t, reply.Modules[0].IsEnabled)
	conflict, err := d.handlePatchExisting(t.Context(), req, row.UserID)
	require.NoError(t, err)
	require.True(t, conflict.Conflict)
	require.Empty(t, conflict.Modules)
}
