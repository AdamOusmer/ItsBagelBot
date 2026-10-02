// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestApplyCreatesBackupsOnlyAsNewPrivateFiles(t *testing.T) {
	nc := testnats.Connect(t)
	t.Setenv("NATS_LEAF_URL", "")
	t.Setenv("NATS_RPC_URL", "")
	t.Setenv("MIGRATION_TEST_NATS_URL", nc.ConnectedUrl())
	t.Setenv("MIGRATION_TEST_DSN", "reader:secret@tcp(127.0.0.1:1)/modules")
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.jsonl")
	require.NoError(t, os.WriteFile(existing, []byte("original backup\n"), 0o600))
	private := filepath.Join(dir, "private.jsonl")
	require.NoError(t, os.WriteFile(private, []byte("private data"), 0o600))
	link := filepath.Join(dir, "link.jsonl")
	require.NoError(t, os.Symlink(private, link))
	tests := []struct {
		name        string
		backup      string
		wantErr     string
		intactFile  string
		wantContent string
	}{
		{name: "creates a new 0600 backup and proceeds to the database", backup: filepath.Join(dir, "new.jsonl"), wantErr: "database page read failed"},
		{name: "never overwrites an existing backup", backup: existing, wantErr: "cannot create backup", intactFile: existing, wantContent: "original backup\n"},
		{name: "never follows a symlink", backup: link, wantErr: "cannot create backup", intactFile: private, wantContent: "private data"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := run(t.Context(), options{apply: true, dsnEnv: "MIGRATION_TEST_DSN", natsEnv: "MIGRATION_TEST_NATS_URL",
				backup: tc.backup, pageSize: 200, timeout: time.Second})

			require.ErrorContains(t, err, tc.wantErr)
			if tc.intactFile != "" {
				got, err := os.ReadFile(tc.intactFile)
				require.NoError(t, err)
				assert.Equal(t, tc.wantContent, string(got))
				return
			}
			info, err := os.Stat(tc.backup)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	}
}

func TestRestoreRejectsJournalIdentityOrRevisionMismatchBeforeDatabaseRead(t *testing.T) {
	prepared := journalEntry{Kind: "prepared", RowID: 1, UserID: 2, Module: "valorant", BeforeRevision: 5, AfterRevision: 6}
	mismatches := map[string]func(*journalEntry){
		"row":      func(e *journalEntry) { e.RowID++ },
		"user":     func(e *journalEntry) { e.UserID++ },
		"module":   func(e *journalEntry) { e.Module = "mcsr" },
		"revision": func(e *journalEntry) { e.BeforeRevision++ },
	}
	for name, mismatch := range mismatches {
		t.Run(name, func(t *testing.T) {
			outcome := prepared
			outcome.Kind = "applied"
			mismatch(&outcome)
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

func TestRestoreOnlyRevertsFieldsThatStillMatchTheMigration(t *testing.T) {
	const (
		bindings = `{"rankMessage":"{valorant:tier}","bindings":[{"message":"hi","enabled":true}]}`
		rewards  = `{"rewards":[{"points":9007199254740992,"message":"{channelpoints:user}"}]}`
	)
	tests := []struct {
		name         string
		migrated     string
		current      string
		wantRestored bool
	}{
		{"reverts equivalent values and keeps an unrelated edit", bindings,
			`{"rankMessage":"{valorant:tier}","bindings":[ { "enabled":true, "message":"hi" } ],"account":"new account"}`, true},
		{"skips an edited template", bindings, `{"rankMessage":"new reply","bindings":[{"message":"hi","enabled":true}]}`, false},
		{"skips an edited nested field", bindings, `{"rankMessage":"{valorant:tier}","bindings":[{"message":"hi","enabled":false}]}`, false},
		{"skips a missing changed key", bindings, `{"rankMessage":"{valorant:tier}"}`, false},
		{"skips an unparsable config", bindings, `{"rankMessage":invalid,"bindings":[]}`, false},
		{"reverts an unchanged large number", rewards, rewards, true},
		{"skips an integer edit beyond float64 precision", rewards, `{"rewards":[{"points":9007199254740993,"message":"{channelpoints:user}"}]}`, false},
		{"skips a value with trailing JSON", rewards, `{"rewards":[] []}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newMigrationFixture(t)
			f.servePatchExisting(t, acceptNextRevision)
			row := f.addModule(2, "valorant", tc.current, 6)
			var after map[string]codec.RawMessage
			require.NoError(t, codec.Unmarshal([]byte(tc.migrated), &after))
			before := map[string]codec.RawMessage{}
			for key := range after {
				before[key] = []byte(`"original"`)
			}
			m, _ := f.applyMigration(t)
			m.opts.restore = appliedRestoreJournal(t, row, before, after)

			require.NoError(t, m.restore(f.ctx))

			wantCounters := map[string]int{"scanned": 1, "changed": 0, "applied": 0, "conflicts": 0, "skipped": 1}
			if tc.wantRestored {
				wantCounters = map[string]int{"scanned": 1, "changed": 1, "applied": 1, "conflicts": 0, "skipped": 0}
			}
			assert.Equal(t, wantCounters, m.counters())
			assert.Len(t, f.requests, map[bool]int{true: 1, false: 0}[tc.wantRestored])
		})
	}
}
