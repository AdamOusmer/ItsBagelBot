// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/app/db/modules/rpc"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const userID = 1001

type dashboard struct {
	nc     *nats.Conn
	client *ent.Client
}

func newDashboard(t *testing.T) dashboard {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewModules(client, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	nc := testnats.Connect(t)
	require.NoError(t, rpc.SubscribeDashboard(rpc.Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()}, Repo: repo}, "modules"))
	return dashboard{nc: nc, client: client}
}

func (d dashboard) call(t *testing.T, verb string, req modulesrpc.DashboardRequest) modulesrpc.DashboardReply {
	t.Helper()
	req.UserID = "1001"
	payload, err := codec.Marshal(req)
	require.NoError(t, err)
	msg, err := d.nc.Request("modules."+verb, payload, 3*time.Second)
	require.NoError(t, err)
	var reply modulesrpc.DashboardReply
	require.NoError(t, codec.Unmarshal(msg.Data, &reply))
	return reply
}

func TestPatchExistingRequiresBothGuardsBeforeTouchingTheRow(t *testing.T) {
	id, rev, invalidID, invalidRev := 1, 0, 0, -1
	for _, tc := range []struct {
		name    string
		id, rev *int
	}{
		{"missing both", nil, nil},
		{"missing ID", nil, &rev},
		{"missing revision", &id, nil},
		{"invalid ID", &invalidID, &rev},
		{"invalid revision", &id, &invalidRev},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDashboard(t)
			row := d.client.Modules.Create().SetUserID(userID).SetName("queue").SetConfigs([]byte(`{"option":"off"}`)).SaveX(t.Context())

			reply := d.call(t, "patch-existing", modulesrpc.DashboardRequest{
				Name: "queue", Configs: codec.RawMessage(`{"option":"on"}`), ExpectedID: tc.id, ExpectedRev: tc.rev,
			})

			assert.Equal(t, domainrpc.CodeInvalid, reply.Code)
			assert.NotEmpty(t, reply.Error)
			assert.Empty(t, reply.Modules)
			assert.JSONEq(t, `{"option":"off"}`, string(d.client.Modules.GetX(t.Context(), row.ID).Configs))
		})
	}
}

func TestPatchExistingReturnsGuardedCommittedRows(t *testing.T) {
	d := newDashboard(t)
	row := d.client.Modules.Create().SetUserID(userID).SetName("queue").SetIsEnabled(false).SetConfigs([]byte(`{"option":"off"}`)).SetRevision(0).SaveX(t.Context())
	req := modulesrpc.DashboardRequest{Name: row.Name, IsEnabled: row.IsEnabled, Configs: codec.RawMessage(`{"option":"on"}`), ExpectedRev: &row.Revision, ExpectedID: &row.ID}

	reply := d.call(t, "patch-existing", req)

	require.Empty(t, reply.Error)
	assert.False(t, reply.Conflict)
	assert.Equal(t, 1, reply.Rev)
	require.Len(t, reply.Modules, 1)
	assert.Equal(t, reply.Rev, reply.Modules[0].Revision)
	assert.False(t, reply.Modules[0].IsEnabled)

	conflict := d.call(t, "patch-existing", req)

	assert.True(t, conflict.Conflict)
	assert.Empty(t, conflict.Modules)
}
