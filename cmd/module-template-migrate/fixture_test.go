// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

const (
	legacyTemplate   = `{"rankMessage":"{tier}","account":"Frosty#EUW1"}`
	migratedTemplate = `{"rankMessage":"{valorant:tier}","account":"Frosty#EUW1"}`
)

type migrationFixture struct {
	ctx      context.Context
	db       *ent.Client
	nc       *nats.Conn
	requests chan modulesrpc.DashboardRequest
}

func newMigrationFixture(t *testing.T) *migrationFixture {
	t.Helper()
	return &migrationFixture{
		ctx:      t.Context(),
		db:       testdb.Open(t, "template-migrate", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) }),
		nc:       testnats.Connect(t),
		requests: make(chan modulesrpc.DashboardRequest, 16),
	}
}

func (f *migrationFixture) addModule(userID uint64, name, configs string, revision int) *ent.Modules {
	return f.db.Modules.Create().SetUserID(userID).SetName(name).SetIsEnabled(true).SetRevision(revision).SetConfigs([]byte(configs)).SaveX(f.ctx)
}

func (f *migrationFixture) servePatchExisting(t *testing.T, reply func(modulesrpc.DashboardRequest) modulesrpc.DashboardReply) {
	t.Helper()
	_, err := f.nc.Subscribe("test.modules.patch-existing", func(msg *nats.Msg) {
		var req modulesrpc.DashboardRequest
		_ = codec.Unmarshal(msg.Data, &req)
		f.requests <- req
		body, _ := codec.Marshal(reply(req))
		_ = msg.Respond(body)
	})
	require.NoError(t, err)
	require.NoError(t, f.nc.Flush())
}

func acceptNextRevision(req modulesrpc.DashboardRequest) modulesrpc.DashboardReply {
	return modulesrpc.DashboardReply{Rev: *req.ExpectedRev + 1}
}

func (f *migrationFixture) applyMigration(t *testing.T) (*migration, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	journal, err := createBackup(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = journal.Close() })
	return &migration{
		opts: options{apply: true, prefix: "test.modules", pageSize: 200, timeout: time.Second},
		db:   f.db, nc: f.nc, journal: journal,
	}, path
}

func (f *migrationFixture) previewMigration(pageSize int) *migration {
	return &migration{opts: options{pageSize: pageSize, timeout: time.Second}, db: f.db}
}

func readJournal(t *testing.T, path string) []journalEntry {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	decoder := codec.NewDecoder(file)
	var entries []journalEntry
	for {
		var entry journalEntry
		if decoder.Decode(&entry) != nil {
			return entries
		}
		entries = append(entries, entry)
	}
}

func journalKinds(entries []journalEntry) []string {
	kinds := make([]string, len(entries))
	for i, entry := range entries {
		kinds[i] = entry.Kind
	}
	return kinds
}

func (m *migration) counters() map[string]int {
	return map[string]int{"scanned": m.scanned, "changed": m.changed, "applied": m.applied, "conflicts": m.conflicts, "skipped": m.skipped}
}
