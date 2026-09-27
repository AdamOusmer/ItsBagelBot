// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	vk "github.com/valkey-io/valkey-go"
)

type warmupClient struct {
	vk.Client
	mu       sync.Mutex
	commands [][]string
	write    bool
	do       func(context.Context, vk.Completed) vk.ValkeyResult
}

func newWarmupClient(t *testing.T) *warmupClient {
	t.Helper()
	client, err := vk.NewClient(vk.ClientOption{
		InitAddress: []string{"unavailable:0"}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true, DialCtxFn: unavailableDial,
	})
	require.ErrorIs(t, err, errOptionalUnavailable)
	require.NotNil(t, client, "ForceSingleClient retains the builder on dial failure")
	t.Cleanup(client.Close)
	return &warmupClient{Client: client}
}

func (c *warmupClient) Do(ctx context.Context, cmd vk.Completed) vk.ValkeyResult {
	c.mu.Lock()
	c.commands = append(c.commands, append([]string(nil), cmd.Commands()...))
	c.write = c.write || !cmd.IsReadOnly()
	c.mu.Unlock()
	if c.do != nil {
		return c.do(ctx, cmd)
	}
	return vk.ValkeyResult{}
}

func TestWarmReadsOnlyProbesLocalRouteWithReadOnlyKeyedCommands(t *testing.T) {
	primary, local := newWarmupClient(t), newWarmupClient(t)
	require.NoError(t, WarmReads(context.Background(), &Client{Client: primary, local: local}))
	require.Empty(t, primary.commands)
	require.Len(t, local.commands, readWarmupProbes)
	require.False(t, local.write)
	for _, cmd := range local.commands {
		require.Equal(t, []string{"HGET", readWarmupKey, "status"}, cmd)
	}
}

func TestWarmReadsSkipsClientsWithoutNodeLocalPool(t *testing.T) {
	primary := newWarmupClient(t)
	require.NoError(t, WarmReads(context.Background(), primary))
	require.NoError(t, WarmReads(context.Background(), &Client{Client: primary}))
	require.Empty(t, primary.commands)
}

func TestWarmReadsAlreadyCancelledDoesNoWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	local := newWarmupClient(t)
	require.ErrorIs(t, WarmReads(ctx, &Client{local: local}), context.Canceled)
	require.Empty(t, local.commands)
}

func TestWarmReadsHonorsDeadlineAndBoundsConcurrency(t *testing.T) {
	var active, peak atomic.Int32
	local := newWarmupClient(t)
	local.do = func(ctx context.Context, cmd vk.Completed) vk.ValkeyResult {
		current := active.Add(1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		defer active.Add(-1)
		<-ctx.Done()
		return local.Client.Do(ctx, cmd)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	require.ErrorIs(t, WarmReads(ctx, &Client{local: local}), context.DeadlineExceeded)
	require.Less(t, time.Since(start), time.Second)
	require.LessOrEqual(t, peak.Load(), int32(readWarmupWorkers))
	require.Zero(t, active.Load(), "all warmup workers must finish before returning")
	require.LessOrEqual(t, len(local.commands), readWarmupWorkers)
}

func TestWarmReadsStopsAfterFirstConnectionFailure(t *testing.T) {
	local := newWarmupClient(t)
	local.do = local.Client.Do
	require.ErrorIs(t, WarmReads(context.Background(), &Client{local: local}), errOptionalUnavailable)
	require.LessOrEqual(t, len(local.commands), readWarmupWorkers)
}

func TestWarmReadsUsesMultiplexedConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	counted := &warmupListener{Listener: ln}
	server := &kvServer{ln: counted, values: map[string]string{}}
	go server.serve()
	local, err := vk.NewClient(vk.ClientOption{
		InitAddress: []string{server.ln.Addr().String()}, AlwaysRESP2: true,
		ForceSingleClient: true, DisableCache: true,
		PipelineMultiplex: localPipelineMultiplex,
	})
	require.NoError(t, err)
	t.Cleanup(local.Close)
	// This drives the real keyed command builder and the library's lazy mux.
	// Exact connection coverage is probabilistic and deliberately not asserted.
	require.NoError(t, WarmReads(context.Background(), &Client{local: local}))
	require.Greater(t, counted.connections.Load(), int32(1), "keyed probes must open connections beyond slot zero")
}

type warmupListener struct {
	net.Listener
	connections atomic.Int32
}

func (l *warmupListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.connections.Add(1)
	}
	return conn, err
}
