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
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/event/data"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestScanPreviewCountsChangesWithoutWritingBackupOrCallingRPC(t *testing.T) {
	f := newMigrationFixture(t)
	f.addModule(2, "valorant", legacyTemplate, 5)
	f.addModule(3, "valorant", migratedTemplate, 1)
	f.addModule(4, "valorant", "not json", 1)
	m := f.previewMigration(1)

	require.NoError(t, m.scan(f.ctx))

	assert.Equal(t, map[string]int{"scanned": 3, "changed": 1, "applied": 0, "conflicts": 0, "skipped": 1}, m.counters())
}

func TestScanApplyPatchesThroughTheRevisionGuardedRPC(t *testing.T) {
	f := newMigrationFixture(t)
	row := f.addModule(2, "valorant", legacyTemplate, 5)
	f.servePatchExisting(t, acceptNextRevision)
	m, journalPath := f.applyMigration(t)

	require.NoError(t, m.scan(f.ctx))

	req := <-f.requests
	require.NotNil(t, req.ExpectedID)
	require.NotNil(t, req.ExpectedRev)
	assert.Equal(t, row.ID, *req.ExpectedID)
	assert.Equal(t, row.Revision, *req.ExpectedRev)
	assert.Equal(t, "2", req.UserID)
	assert.Equal(t, row.IsEnabled, req.IsEnabled)
	assert.JSONEq(t, `{"rankMessage":"{valorant:tier}"}`, string(req.Configs))
	assert.Equal(t, 1, m.applied)
	entries := readJournal(t, journalPath)
	assert.Equal(t, []string{"prepared", "applied"}, journalKinds(entries))
	assert.JSONEq(t, `"{tier}"`, string(entries[0].Before["rankMessage"]))
}

func TestScanApplyStopsBeforeRPCWhenTheBackupCannotBeWritten(t *testing.T) {
	f := newMigrationFixture(t)
	f.addModule(2, "valorant", legacyTemplate, 5)
	f.servePatchExisting(t, acceptNextRevision)
	m, _ := f.applyMigration(t)
	require.NoError(t, m.journal.Close())

	err := m.scan(f.ctx)

	assert.ErrorContains(t, err, "backup write failed")
	assert.Empty(t, f.requests, "no patch may be sent without a backup")
	assert.Zero(t, m.applied)
}

func TestScanApplyNeverFallsBackToTheLegacyPatchSubject(t *testing.T) {
	f := newMigrationFixture(t)
	f.addModule(2, "valorant", legacyTemplate, 0)
	var legacyCalls atomic.Int32
	_, err := f.nc.Subscribe("test.modules.patch", func(msg *nats.Msg) {
		legacyCalls.Add(1)
		_ = msg.Respond([]byte(`{"rev":1}`))
	})
	require.NoError(t, err)
	require.NoError(t, f.nc.Flush())
	m, journalPath := f.applyMigration(t)

	err = m.scan(f.ctx)

	require.ErrorContains(t, err, "outcome is uncertain")
	assert.Zero(t, legacyCalls.Load(), "older modules services must not receive a potentially unguarded mutation")
	assert.Zero(t, m.applied)
	assert.Equal(t, []string{"prepared", "uncertain"}, journalKinds(readJournal(t, journalPath)))
}

type restoreRaceFixture struct {
	*migrationFixture
	pub  *bustest.Publisher
	repo *repository.Modules
	row  *ent.Modules
}

func TestRestoreCannotRecreateOrOverwriteRowReplacedAfterRead(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprintf("replace=%t", replace), func(t *testing.T) {
			verifyRestoreRace(t, replace)
		})
	}
}

func newRestoreRaceFixture(t *testing.T) restoreRaceFixture {
	t.Helper()
	f := newMigrationFixture(t)
	pub := bustest.NewPublisher()
	repo := repository.NewModules(f.db, pub, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	return restoreRaceFixture{migrationFixture: f, pub: pub, repo: repo,
		row: f.addModule(2, "valorant", `{"rankMessage":"{valorant:tier}","__rev":1}`, 1)}
}

func (f restoreRaceFixture) serveReplacement(t *testing.T, replace bool) chan error {
	t.Helper()
	callbackErrors := make(chan error, 1)
	f.servePatchExisting(t, func(req modulesrpc.DashboardRequest) modulesrpc.DashboardReply {
		result, err := f.patchAfterReplacement(req, replace)
		callbackErrors <- err
		return modulesrpc.DashboardReply{Rev: result.Rev, Conflict: result.Conflict}
	})
	return callbackErrors
}

func (f restoreRaceFixture) patchAfterReplacement(req modulesrpc.DashboardRequest, replace bool) (repository.PatchResult, error) {
	if err := f.replaceRow(replace); err != nil {
		return repository.PatchResult{}, err
	}
	if req.ExpectedRev == nil || req.ExpectedID == nil {
		return repository.PatchResult{}, fmt.Errorf("missing guards")
	}
	var partial map[string]codec.RawMessage
	if err := codec.Unmarshal(req.Configs, &partial); err != nil {
		return repository.PatchResult{}, err
	}
	return f.repo.PatchExisting(f.ctx, f.row.UserID, req.Name, req.IsEnabled, partial, *req.ExpectedRev, *req.ExpectedID)
}

func (f restoreRaceFixture) replaceRow(replace bool) error {
	if err := f.db.Modules.DeleteOneID(f.row.ID).Exec(f.ctx); err != nil {
		return err
	}
	if !replace {
		return nil
	}
	return f.db.Modules.Create().SetUserID(f.row.UserID).SetName(f.row.Name).SetIsEnabled(false).SetRevision(f.row.Revision).SetConfigs(f.row.Configs).Exec(f.ctx)
}

func appliedRestoreJournal(t *testing.T, row *ent.Modules, before, after map[string]codec.RawMessage) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "original.jsonl")
	file, err := createBackup(path)
	require.NoError(t, err)
	prepared := journalEntry{Kind: "prepared", RowID: row.ID, UserID: row.UserID, Module: row.Name, BeforeRevision: row.Revision - 1, AfterRevision: row.Revision,
		Before: before, After: after}
	encoder := codec.NewEncoder(file)
	require.NoError(t, encoder.Encode(prepared))
	prepared.Kind = "applied"
	require.NoError(t, encoder.Encode(prepared))
	require.NoError(t, file.Close())
	return path
}

func verifyRestoreRace(t *testing.T, replace bool) {
	t.Helper()
	f := newRestoreRaceFixture(t)
	callbackErrors := f.serveReplacement(t, replace)
	source := appliedRestoreJournal(t, f.row,
		map[string]codec.RawMessage{"rankMessage": []byte(`"{tier}"`)}, map[string]codec.RawMessage{"rankMessage": []byte(`"{valorant:tier}"`)})
	rollback, err := createBackup(filepath.Join(t.TempDir(), "rollback.jsonl"))
	require.NoError(t, err)
	defer rollback.Close()
	m := migration{opts: options{apply: true, restore: source, prefix: "test.modules", timeout: time.Second}, db: f.db, nc: f.nc, journal: rollback}

	require.NoError(t, m.restore(f.ctx))

	req := <-f.requests
	require.NoError(t, <-callbackErrors)
	require.NotNil(t, req.ExpectedID)
	assert.Equal(t, f.row.ID, *req.ExpectedID)
	assert.Equal(t, 1, m.conflicts)
	assert.Zero(t, m.applied)
	assert.Empty(t, f.pub.On(data.SubjectModuleChanged))
	f.assertRemainingRows(t, replace)
}

func (f restoreRaceFixture) assertRemainingRows(t *testing.T, replace bool) {
	t.Helper()
	rows := f.db.Modules.Query().AllX(f.ctx)
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
