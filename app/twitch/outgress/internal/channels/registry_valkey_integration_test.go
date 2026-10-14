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
	"ItsBagelBot/internal/valkeytest"
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

	require.NoError(t, r.SetFlags(ctx, "42", &enabled, nil))
	require.NoError(t, r.SetSubState(ctx, "42", "revoked", ""))
	require.NoError(t, r.SetGrantState(ctx, "42", manage.GrantDead))

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

func TestGetReadsBlockedAndGrantCheckedTimestamps(t *testing.T) {
	r := New(channelsRealValkey(t))
	ctx := context.Background()

	require.NoError(t, r.SetGrantState(ctx, "1", manage.GrantDead))
	require.NoError(t, r.MarkBlocked(ctx, manage.Channel{BroadcasterID: "1"}))
	require.NoError(t, r.SetFlags(ctx, "2", &enabled, nil))

	blocked, _, err := r.Get(ctx, "1")
	require.NoError(t, err)
	require.False(t, blocked.BlockedAt.IsZero())
	require.False(t, blocked.GrantCheckedAt.IsZero())

	plain, _, err := r.Get(ctx, "2")
	require.NoError(t, err)
	require.True(t, plain.BlockedAt.IsZero())
	require.True(t, plain.GrantCheckedAt.IsZero())
}

func unhealthyMembers(t *testing.T, r *Registry) []string {
	t.Helper()
	ids, err := r.UnhealthyBatch(context.Background(), 100)
	require.NoError(t, err)
	return ids
}

func TestUnhealthySetFollowsSubStateTransitions(t *testing.T) {
	r := New(valkeytest.Spawn(t))
	ctx := context.Background()

	require.NoError(t, r.SetSubState(ctx, "42", "failing", "missing"))
	require.Equal(t, []string{"42"}, unhealthyMembers(t, r))
	require.NoError(t, r.SetSubState(ctx, "42", "pending", ""))
	require.Equal(t, []string{"42"}, unhealthyMembers(t, r))
	require.NoError(t, r.SetSubState(ctx, "42", "ok", ""))
	require.Empty(t, unhealthyMembers(t, r))

	require.NoError(t, r.SetSubState(ctx, "42", "failing", "x"))
	require.NoError(t, r.SetSubState(ctx, "42", "revoked", ""))
	require.Empty(t, unhealthyMembers(t, r))
}

func TestUnhealthySetFollowsSetSubStateUnless(t *testing.T) {
	r := New(valkeytest.Spawn(t))
	ctx := context.Background()

	applied, err := r.SetSubStateUnless(ctx, "42", SubState{State: "failing", Error: "x"}, "revoked", "chat_banned")
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, []string{"42"}, unhealthyMembers(t, r))

	applied, err = r.SetSubStateUnless(ctx, "42", SubState{State: "ok", Error: ""}, "revoked", "chat_banned")
	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, unhealthyMembers(t, r))

	require.NoError(t, r.SetSubState(ctx, "7", "chat_banned", ""))
	require.NoError(t, r.SetSubState(ctx, "8", "failing", ""))
	applied, err = r.SetSubStateUnless(ctx, "7", SubState{State: "failing", Error: "x"}, "revoked", "chat_banned")
	require.NoError(t, err)
	require.False(t, applied)
	require.Equal(t, []string{"8"}, unhealthyMembers(t, r))
}

func TestUnhealthySetDropsErasedAndExpiredChannels(t *testing.T) {
	client := valkeytest.Spawn(t)
	r := New(client)
	ctx := context.Background()

	require.NoError(t, r.SetSubState(ctx, "1", "failing", ""))
	require.NoError(t, r.SetSubState(ctx, "2", "failing", ""))
	require.NoError(t, r.SetIncarnation(ctx, "1", 100))
	require.NoError(t, r.EraseIncarnation(ctx, "1", 100))
	require.Equal(t, []string{"2"}, unhealthyMembers(t, r))

	require.NoError(t, client.Do(ctx, client.B().Del().Key(keyPrefix+"2").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Zadd().Key(hotIndexKey).ScoreMember().ScoreMember(float64(time.Now().Add(-time.Hour).Unix()), "2").Build()).Error())
	require.NoError(t, r.PruneHot(ctx))
	require.Empty(t, unhealthyMembers(t, r))
}
