// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
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
			verifyRestoreRace(t, replace)
		})
	}
}

type restoreRaceFixture struct {
	ctx            context.Context
	client         *ent.Client
	pub            *bustest.Publisher
	repo           *repository.Modules
	nc             *nats.Conn
	row            *ent.Modules
	requests       chan modulesrpc.DashboardRequest
	callbackErrors chan error
}

func newRestoreRaceFixture(t *testing.T) restoreRaceFixture {
	t.Helper()
	ctx := t.Context()
	client := testdb.Open(t, "template-migrate-restore", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	pub := bustest.NewPublisher()
	repo := repository.NewModules(client, pub, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	row := client.Modules.Create().SetUserID(2).SetName("valorant").SetIsEnabled(true).SetRevision(1).SetConfigs([]byte(`{"rankMessage":"{valorant:tier}","__rev":1}`)).SaveX(ctx)
	return restoreRaceFixture{ctx: ctx, client: client, pub: pub, repo: repo, nc: migrationTestNATS(t), row: row,
		requests: make(chan modulesrpc.DashboardRequest, 1), callbackErrors: make(chan error, 1)}
}

func (f restoreRaceFixture) serveReplacement(t *testing.T, replace bool) {
	t.Helper()
	_, err := f.nc.Subscribe("test.modules.patch-existing", func(msg *nats.Msg) {
		req, result, err := f.patchAfterReplacement(msg.Data, replace)
		f.requests <- req
		f.callbackErrors <- err
		body, _ := codec.Marshal(modulesrpc.DashboardReply{Rev: result.Rev, Conflict: result.Conflict})
		_ = msg.Respond(body)
	})
	require.NoError(t, err)
	require.NoError(t, f.nc.Flush())
}

func (f restoreRaceFixture) patchAfterReplacement(data []byte, replace bool) (modulesrpc.DashboardRequest, repository.PatchResult, error) {
	var req modulesrpc.DashboardRequest
	if err := codec.Unmarshal(data, &req); err != nil {
		return req, repository.PatchResult{}, err
	}
	if err := f.replaceRow(replace); err != nil {
		return req, repository.PatchResult{}, err
	}
	if req.ExpectedRev == nil || req.ExpectedID == nil {
		return req, repository.PatchResult{}, fmt.Errorf("missing guards")
	}
	var partial map[string]codec.RawMessage
	if err := codec.Unmarshal(req.Configs, &partial); err != nil {
		return req, repository.PatchResult{}, err
	}
	result, err := f.repo.PatchExisting(f.ctx, f.row.UserID, req.Name, req.IsEnabled, partial, *req.ExpectedRev, *req.ExpectedID)
	return req, result, err
}

func (f restoreRaceFixture) replaceRow(replace bool) error {
	if err := f.client.Modules.DeleteOneID(f.row.ID).Exec(f.ctx); err != nil {
		return err
	}
	if !replace {
		return nil
	}
	return f.client.Modules.Create().SetUserID(f.row.UserID).SetName(f.row.Name).SetIsEnabled(false).SetRevision(f.row.Revision).SetConfigs(f.row.Configs).Exec(f.ctx)
}

func appliedRestoreJournal(t *testing.T, row *ent.Modules) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "original.jsonl")
	file, err := createBackup(path)
	require.NoError(t, err)
	prepared := journalEntry{Kind: "prepared", RowID: row.ID, UserID: row.UserID, Module: row.Name, BeforeRevision: 0, AfterRevision: 1,
		Before: map[string]codec.RawMessage{"rankMessage": []byte(`"{tier}"`)}, After: map[string]codec.RawMessage{"rankMessage": []byte(`"{valorant:tier}"`)}}
	encoder := codec.NewEncoder(file)
	require.NoError(t, encoder.Encode(prepared))
	prepared.Kind = "applied"
	require.NoError(t, encoder.Encode(prepared))
	require.NoError(t, file.Close())
	return path
}

func verifyRestoreRace(t *testing.T, replace bool) {
	t.Helper()
	fixture := newRestoreRaceFixture(t)
	fixture.serveReplacement(t, replace)
	source := appliedRestoreJournal(t, fixture.row)
	rollback, err := createBackup(filepath.Join(t.TempDir(), "rollback.jsonl"))
	require.NoError(t, err)
	defer rollback.Close()
	m := migration{opts: options{apply: true, restore: source, prefix: "test.modules", timeout: time.Second}, db: fixture.client, nc: fixture.nc, journal: rollback}
	require.NoError(t, m.restore(fixture.ctx))
	req := <-fixture.requests
	require.NoError(t, <-fixture.callbackErrors)
	require.NotNil(t, req.ExpectedID)
	assert.Equal(t, fixture.row.ID, *req.ExpectedID)
	assert.Equal(t, 1, m.conflicts)
	assert.Zero(t, m.applied)
	assert.Empty(t, fixture.pub.On(data.SubjectModuleChanged))
	fixture.assertRemainingRows(t, replace)
}

func (f restoreRaceFixture) assertRemainingRows(t *testing.T, replace bool) {
	t.Helper()
	rows := f.client.Modules.Query().AllX(f.ctx)
	if !replace {
		assert.Empty(t, rows)
		return
	}
	require.Len(t, rows, 1)
	assert.NotEqual(t, f.row.ID, rows[0].ID)
	assert.Equal(t, f.row.Revision, rows[0].Revision)
	assert.False(t, rows[0].IsEnabled)
	assert.JSONEq(t, string(f.row.Configs), string(rows[0].Configs))
}
