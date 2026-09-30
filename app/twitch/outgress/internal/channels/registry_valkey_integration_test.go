// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channels

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"

	"ItsBagelBot/internal/domain/rpc/manage"
)

func channelsRealValkey(t *testing.T) valkey_go.Client {
	t.Helper()
	binary, err := exec.LookPath("valkey-server")
	if err != nil {
		t.Skip("valkey-server is not installed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	dir := t.TempDir()
	server := exec.Command(binary, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", dir, "--logfile", filepath.Join(dir, "valkey.log"))
	require.NoError(t, server.Start())
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	address := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("temporary Valkey did not start: %v", dialErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	client, err := valkey_go.NewClient(valkey_go.ClientOption{InitAddress: []string{address}, DisableCache: true})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func TestMarkBlockedDoesNotRevertAConcurrentSetSubState(t *testing.T) {
	client := channelsRealValkey(t)
	r := New(client)
	ctx := context.Background()

	require.NoError(t, r.Save(ctx, manage.Channel{
		BroadcasterID: "42",
		Enabled:       true,
		SubState:      "revoked",
		GrantState:    manage.GrantDead,
	}))

	require.NoError(t, r.SetSubState(ctx, "42", "banned", "concurrent update"))
	require.NoError(t, r.MarkBlocked(ctx, manage.Channel{BroadcasterID: "42"}))

	ch, found, err := r.Get(ctx, "42")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "banned", ch.SubState)
	require.Equal(t, "concurrent update", ch.SubError)
	require.Equal(t, manage.GrantDead, ch.GrantState)
	require.False(t, ch.BlockedAt.IsZero())
}
