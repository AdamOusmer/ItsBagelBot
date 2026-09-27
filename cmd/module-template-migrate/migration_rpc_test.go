// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/event/data"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func migrationTestNATS(t *testing.T) *nats.Conn {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	srv.Start()
	require.True(t, srv.ReadyForConnections(5*time.Second))
	t.Cleanup(srv.Shutdown)
	nc, err := nats.Connect(srv.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	return nc
}

func TestPatchExistingCannotReachLegacyPatchServer(t *testing.T) {
	nc := migrationTestNATS(t)
	var legacyCalls atomic.Int32
	_, err := nc.Subscribe("test.modules.patch", func(msg *nats.Msg) {
		legacyCalls.Add(1)
		_ = msg.Respond([]byte(`{"rev":1}`))
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	journal, err := createBackup(filepath.Join(t.TempDir(), "journal.jsonl"))
	require.NoError(t, err)
	defer journal.Close()
	m := migration{opts: options{apply: true, prefix: "test.modules", timeout: time.Second}, nc: nc, journal: journal}
	row := &ent.Modules{ID: 1, UserID: 2, Name: "valorant", Revision: 0, Configs: []byte(`{"rankMessage":"{tier}"}`)}
	err = m.patch(t.Context(), row, map[string]codec.RawMessage{"rankMessage": []byte(`"{valorant:tier}"`)})
	require.ErrorContains(t, err, "outcome is uncertain")
	assert.Zero(t, legacyCalls.Load(), "older modules services must not receive a potentially unguarded mutation")
	assert.Zero(t, m.applied)
}

func TestPatchExistingSendsRowIDAndRevision(t *testing.T) {
	nc := migrationTestNATS(t)
	requests := make(chan modulesrpc.DashboardRequest, 1)
	_, err := nc.Subscribe("test.modules.patch-existing", func(msg *nats.Msg) {
		var req modulesrpc.DashboardRequest
		_ = codec.Unmarshal(msg.Data, &req)
		requests <- req
		_ = msg.Respond([]byte(`{"rev":6}`))
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	journal, err := createBackup(filepath.Join(t.TempDir(), "journal.jsonl"))
	require.NoError(t, err)
	defer journal.Close()
	m := migration{opts: options{apply: true, prefix: "test.modules", timeout: time.Second}, nc: nc, journal: journal}
	row := &ent.Modules{ID: 10, UserID: 2, Name: "valorant", Revision: 5, Configs: []byte(`{"rankMessage":"{tier}"}`)}
	require.NoError(t, m.patch(t.Context(), row, map[string]codec.RawMessage{"rankMessage": []byte(`"{valorant:tier}"`)}))
	req := <-requests
	require.NotNil(t, req.ExpectedID)
	require.NotNil(t, req.ExpectedRev)
	assert.Equal(t, row.ID, *req.ExpectedID)
	assert.Equal(t, row.Revision, *req.ExpectedRev)
	assert.Equal(t, "2", req.UserID)
	assert.Equal(t, row.IsEnabled, req.IsEnabled)
	assert.Equal(t, 1, m.applied)
}

func TestRestoreCannotRecreateOrOverwriteRowReplacedAfterRead(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprintf("replace=%t", replace), func(t *testing.T) {
			ctx := t.Context()
			client := testdb.Open(t, "template-migrate-restore", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
			pub := bustest.NewPublisher()
			repo := repository.NewModules(client, pub, nil, zap.NewNop())
			defer repo.Close(ctx)
			nc := migrationTestNATS(t)
			row := client.Modules.Create().SetUserID(2).SetName("valorant").SetIsEnabled(true).SetRevision(1).SetConfigs([]byte(`{"rankMessage":"{valorant:tier}","__rev":1}`)).SaveX(ctx)
			requests := make(chan modulesrpc.DashboardRequest, 1)
			callbackErrors := make(chan error, 1)
			_, err := nc.Subscribe("test.modules.patch-existing", func(msg *nats.Msg) {
				var req modulesrpc.DashboardRequest
				callbackErr := codec.Unmarshal(msg.Data, &req)
				if callbackErr == nil {
					callbackErr = client.Modules.DeleteOneID(row.ID).Exec(ctx)
				}
				if callbackErr == nil && replace {
					callbackErr = client.Modules.Create().SetUserID(row.UserID).SetName(row.Name).SetIsEnabled(false).SetRevision(row.Revision).SetConfigs(row.Configs).Exec(ctx)
				}
				var result repository.PatchResult
				if callbackErr == nil {
					var partial map[string]codec.RawMessage
					callbackErr = codec.Unmarshal(req.Configs, &partial)
					if callbackErr == nil && req.ExpectedRev != nil && req.ExpectedID != nil {
						result, callbackErr = repo.PatchExisting(ctx, row.UserID, req.Name, req.IsEnabled, partial, *req.ExpectedRev, *req.ExpectedID)
					} else if callbackErr == nil {
						callbackErr = fmt.Errorf("missing guards")
					}
				}
				requests <- req
				callbackErrors <- callbackErr
				body, _ := codec.Marshal(modulesrpc.DashboardReply{Rev: result.Rev, Conflict: result.Conflict})
				_ = msg.Respond(body)
			})
			require.NoError(t, err)
			require.NoError(t, nc.Flush())
			source := filepath.Join(t.TempDir(), "original.jsonl")
			original, err := createBackup(source)
			require.NoError(t, err)
			prepared := journalEntry{Kind: "prepared", RowID: row.ID, UserID: row.UserID, Module: row.Name, BeforeRevision: 0, AfterRevision: 1,
				Before: map[string]codec.RawMessage{"rankMessage": []byte(`"{tier}"`)}, After: map[string]codec.RawMessage{"rankMessage": []byte(`"{valorant:tier}"`)}}
			encoder := codec.NewEncoder(original)
			require.NoError(t, encoder.Encode(prepared))
			prepared.Kind = "applied"
			require.NoError(t, encoder.Encode(prepared))
			require.NoError(t, original.Close())
			rollback, err := createBackup(filepath.Join(t.TempDir(), "rollback.jsonl"))
			require.NoError(t, err)
			defer rollback.Close()
			m := migration{opts: options{apply: true, restore: source, prefix: "test.modules", timeout: time.Second}, db: client, nc: nc, journal: rollback}
			require.NoError(t, m.restore(ctx))
			req := <-requests
			require.NoError(t, <-callbackErrors)
			require.NotNil(t, req.ExpectedID)
			assert.Equal(t, row.ID, *req.ExpectedID)
			assert.Equal(t, 1, m.conflicts)
			assert.Zero(t, m.applied)
			assert.Empty(t, pub.On(data.SubjectModuleChanged))
			rows := client.Modules.Query().AllX(ctx)
			if !replace {
				assert.Empty(t, rows)
				return
			}
			require.Len(t, rows, 1)
			assert.NotEqual(t, row.ID, rows[0].ID)
			assert.Equal(t, row.Revision, rows[0].Revision)
			assert.False(t, rows[0].IsEnabled)
			assert.JSONEq(t, string(row.Configs), string(rows[0].Configs))
		})
	}
}
