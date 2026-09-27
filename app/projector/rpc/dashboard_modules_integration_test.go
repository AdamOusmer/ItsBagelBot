// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func moduleTestValkey(t *testing.T) valkey.Client {
	t.Helper()
	binary, err := exec.LookPath("valkey-server")
	if err != nil {
		t.Skip("valkey-server is not installed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	dir := t.TempDir()
	server := exec.Command(binary, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", dir, "--logfile", filepath.Join(dir, "valkey.log"))
	require.NoError(t, server.Start())
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 3*time.Second, 10*time.Millisecond)
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{address}, DisableCache: true})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func TestModuleSaveProjectsCommittedSnapshotBeforeReply(t *testing.T) {
	store := projection.NewStore(moduleTestValkey(t))
	ctx := t.Context()
	client := testdb.Open(t, "module-toggle-projector", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewModules(client, bustest.NewPublisher(), nil, zap.NewNop())
	defer repo.Close(ctx)
	repo.SetAccountInstanceResolver(func(_ context.Context, _ uint64) (int64, error) { return 100, nil })
	loyalty, err := repo.SetNow(ctx, 1001, "loyalty", true, codec.RawMessage(`{}`))
	require.NoError(t, err)
	require.NoError(t, store.SetModules(ctx, 1001, loyalty))
	before, err := repo.SetNow(ctx, 1001, "queue", false, codec.RawMessage(`{}`))
	require.NoError(t, err)
	require.NoError(t, store.SetModules(ctx, 1001, before))
	committed, err := repo.SetNow(ctx, 1001, "queue", true, codec.RawMessage(`{}`))
	require.NoError(t, err)
	d := &Dashboard{store: store, writeGate: make(chan struct{}, 1)}
	reply := d.handleModulesReplace(ctx, projectorrpc.DashboardRequest{UserID: "1001", Modules: committed})
	require.Empty(t, reply.Error)
	actual, projected, err := store.GetModulesPrimary(ctx, 1001)
	require.NoError(t, err)
	require.True(t, projected)
	require.True(t, actual["queue"].IsEnabled)
	require.Equal(t, 2, actual["queue"].Revision)
	require.Equal(t, int64(100), actual["loyalty"].AccountCreatedAt)

	// A delayed replacement must echo the accepted state, not falsely
	// acknowledge rows rejected by the Lua revision fence.
	stale := d.handleModulesReplace(ctx, projectorrpc.DashboardRequest{UserID: "1001", Modules: before})
	require.Empty(t, stale.Error)
	require.ElementsMatch(t, projection.ModuleList(actual), stale.Modules)

	// Projection failures must not report the proposed snapshot as synchronized.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	failed := d.handleModulesReplace(cancelled, projectorrpc.DashboardRequest{UserID: "1001", Modules: committed})
	require.NotEmpty(t, failed.Error)
	require.Empty(t, failed.Modules)
}
