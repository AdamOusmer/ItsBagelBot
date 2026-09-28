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
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateBackupRequiresNewPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	file, err := createBackup(path)
	require.NoError(t, err)
	defer file.Close()
	info, err := file.Stat()
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	_, err = file.WriteString("original backup\n")
	require.NoError(t, err)
	require.NoError(t, file.Sync())
	repeated, err := createBackup(path)
	assert.Error(t, err)
	if repeated != nil {
		repeated.Close()
	}
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "original backup\n", string(got), "existing backups must never be overwritten")
}

func TestCreateBackupRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "private.jsonl")
	require.NoError(t, os.WriteFile(target, []byte("private data"), 0600))
	link := filepath.Join(dir, "backup.jsonl")
	require.NoError(t, os.Symlink(target, link))
	file, err := createBackup(link)
	assert.Error(t, err)
	if file != nil {
		file.Close()
	}
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "private data", string(got))
}

func TestMigrationRPCURLLeavesLeafRoutingToBus(t *testing.T) {
	for _, tc := range []struct {
		name, leaf, rpc, fallback, want string
		wantErr                         bool
	}{
		{name: "fallback", fallback: "nats://fallback:4222", want: "nats://fallback:4222"},
		{name: "rpc override", rpc: "nats://rpc:4222", fallback: "nats://fallback:4222", want: "nats://rpc:4222"},
		{name: "leaf keeps rpc url", leaf: "nats://leaf:4222", fallback: "nats://fallback:4222", want: "nats://fallback:4222"},
		{name: "leaf only", leaf: "nats://leaf:4222"},
		{name: "none", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NATS_LEAF_URL", tc.leaf)
			t.Setenv("NATS_RPC_URL", tc.rpc)
			t.Setenv("MIGRATION_TEST_NATS_URL", tc.fallback)
			got, err := migrationRPCURL("MIGRATION_TEST_NATS_URL")
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestApplyRequiresBackupBeforeOpeningExternalServices(t *testing.T) {
	err := run(context.Background(), options{apply: true, pageSize: 200, timeout: time.Second})
	assert.ErrorContains(t, err, "--apply requires --backup")
}

func TestPatchStopsBeforeRPCWhenBackupCannotBeWritten(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "journal-*")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	m := migration{opts: options{apply: true}, journal: file}
	row := &ent.Modules{ID: 1, UserID: 2, Name: "valorant", Revision: 5, Configs: []byte(`{"rankMessage":"{tier}","account":"Frosty#EUW1"}`)}
	err = m.patch(context.Background(), row, map[string]codec.RawMessage{"rankMessage": []byte(`"{valorant:tier}"`)})
	assert.ErrorContains(t, err, "backup write failed")
	assert.Zero(t, m.applied)
}

func TestPatchRejectsMissingOriginalKeyBeforeRPC(t *testing.T) {
	m := migration{opts: options{apply: true}}
	row := &ent.Modules{ID: 1, UserID: 2, Name: "valorant", Configs: []byte(`{"account":"Frosty#EUW1"}`)}
	err := m.patch(context.Background(), row, map[string]codec.RawMessage{"rankMessage": []byte(`"{valorant:tier}"`)})
	assert.ErrorContains(t, err, "missing key")
	assert.Zero(t, m.applied)
}

func TestDryRunDoesNotWriteBackupOrCallRPC(t *testing.T) {
	m := migration{}
	row := &ent.Modules{ID: 1, UserID: 2, Name: "valorant", Revision: 5, Configs: []byte(`{"rankMessage":"{tier}"}`)}
	require.NoError(t, m.patch(context.Background(), row, map[string]codec.RawMessage{"rankMessage": []byte(`"{valorant:tier}"`)}))
	assert.Equal(t, 1, m.changed)
	assert.Zero(t, m.applied)
}

func TestRestoreChangedValuesRejectsEditsAndAllowsUnrelatedChanges(t *testing.T) {
	expected := map[string]codec.RawMessage{"rankMessage": []byte(`"{valorant:tier}"`), "bindings": []byte(`[{"message":"hi","enabled":true}]`)}
	for _, tc := range []struct {
		name    string
		current map[string]codec.RawMessage
		want    bool
	}{
		{"equivalent values with unrelated edit", map[string]codec.RawMessage{"rankMessage": []byte(`"{valorant:tier}"`), "bindings": []byte(`[ { "enabled":true, "message":"hi" } ]`), "account": []byte(`"new account"`)}, true},
		{"edited template", map[string]codec.RawMessage{"rankMessage": []byte(`"new reply"`), "bindings": expected["bindings"]}, false},
		{"edited nested field", map[string]codec.RawMessage{"rankMessage": expected["rankMessage"], "bindings": []byte(`[{"message":"hi","enabled":false}]`)}, false},
		{"missing changed key", map[string]codec.RawMessage{"rankMessage": expected["rankMessage"]}, false},
		{"invalid current JSON", map[string]codec.RawMessage{"rankMessage": []byte(`invalid`), "bindings": expected["bindings"]}, false},
	} {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, sameChangedValues(tc.current, expected)) })
	}
	assert.True(t, sameKeys(expected, map[string]codec.RawMessage{"rankMessage": nil, "bindings": nil}))
	assert.False(t, sameKeys(expected, map[string]codec.RawMessage{"rankMessage": nil, "different": nil}))
	assert.False(t, sameKeys(expected, map[string]codec.RawMessage{"rankMessage": nil}))
}

func TestRestoreRejectsJournalIdentityOrRevisionMismatchBeforeDatabaseRead(t *testing.T) {
	prepared := journalEntry{Kind: "prepared", RowID: 1, UserID: 2, Module: "valorant", BeforeRevision: 5, AfterRevision: 6}
	for _, mismatch := range []string{"row", "user", "module", "revision"} {
		t.Run(mismatch, func(t *testing.T) {
			outcome := prepared
			outcome.Kind = "applied"
			switch mismatch {
			case "row":
				outcome.RowID++
			case "user":
				outcome.UserID++
			case "module":
				outcome.Module = "mcsr"
			case "revision":
				outcome.BeforeRevision++
			}
			path := filepath.Join(t.TempDir(), "journal.jsonl")
			file, err := os.Create(path)
			require.NoError(t, err)
			encoder := codec.NewEncoder(file)
			require.NoError(t, encoder.Encode(prepared))
			require.NoError(t, encoder.Encode(outcome))
			require.NoError(t, file.Close())
			m := migration{opts: options{restore: path}}
			assert.ErrorContains(t, m.restore(context.Background()), "outcome does not match")
		})
	}
}

func TestRestoreChangedValuesPreservesLargeNumberEdits(t *testing.T) {
	expected := map[string]codec.RawMessage{"rewards": []byte(`[{"points":9007199254740992,"message":"{channelpoints:user}"}]`)}
	changed := map[string]codec.RawMessage{"rewards": []byte(`[{"points":9007199254740993,"message":"{channelpoints:user}"}]`)}
	assert.False(t, sameChangedValues(changed, expected), "rollback must detect integer edits beyond float64 precision")
	assert.True(t, sameChangedValues(expected, expected))
	assert.False(t, sameChangedValues(map[string]codec.RawMessage{"rewards": []byte(`[] []`)}, expected), "invalid trailing JSON must not compare as a valid value")
}
