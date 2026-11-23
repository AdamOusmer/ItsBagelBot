// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

type countingClient struct {
	valkey_go.Client
	calls atomic.Int64
}

func (c *countingClient) Do(ctx context.Context, cmd valkey_go.Completed) valkey_go.ValkeyResult {
	c.calls.Add(1)
	return c.Client.Do(ctx, cmd)
}

func (c *countingClient) DoMulti(ctx context.Context, multi ...valkey_go.Completed) []valkey_go.ValkeyResult {
	c.calls.Add(1)
	return c.Client.DoMulti(ctx, multi...)
}

func closedListenerAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func newDeadLocalClient(t *testing.T) *countingClient {
	t.Helper()
	opts := (localReadConfig{address: closedListenerAddr(t)}).option()
	opts.ForceSingleClient = true
	raw, _ := valkey_go.NewClient(opts)
	t.Cleanup(raw.Close)
	return &countingClient{Client: raw}
}

func TestDoFallsOverToPrimaryOnALocalConnectionErrorThenSkipsLocalDuringCooldown(t *testing.T) {
	primary := newFakeValkey(t)
	primary.put("k", "v")
	local := newDeadLocalClient(t)
	client := &Client{Client: primary.client, local: local}

	start := time.Now()
	got, err := client.Do(context.Background(), primary.client.B().Get().Key("k").Build()).ToString()
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "v", got, "a dead local client must still read the primary's value")
	assert.Less(t, elapsed, time.Second, "the bounded local dial must not hold up the read")
	assert.Equal(t, int64(1), local.calls.Load())
	assert.Positive(t, client.localSkipUntil.Load(), "a connection error on local must start the cooldown")

	gotAgain, err := client.Do(context.Background(), primary.client.B().Get().Key("k").Build()).ToString()

	require.NoError(t, err)
	assert.Equal(t, "v", gotAgain)
	assert.Equal(t, int64(1), local.calls.Load(), "a read during the cooldown must skip the local client entirely")
}

func TestDoMultiFallsOverToPrimaryOnALocalConnectionError(t *testing.T) {
	primary := newFakeValkey(t)
	primary.put("k1", "v1")
	primary.put("k2", "v2")
	local := newDeadLocalClient(t)
	client := &Client{Client: primary.client, local: local}

	results := client.DoMulti(context.Background(),
		primary.client.B().Get().Key("k1").Build(),
		primary.client.B().Get().Key("k2").Build(),
	)

	require.Len(t, results, 2)
	v1, err1 := results[0].ToString()
	v2, err2 := results[1].ToString()
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, "v1", v1)
	assert.Equal(t, "v2", v2)
	assert.Equal(t, int64(1), local.calls.Load())
}

func TestLocalRetriesAfterTheCooldownElapses(t *testing.T) {
	client := &Client{local: &countingClient{}}

	assert.True(t, client.localAvailable(), "no prior failure must not withhold local")

	client.markLocalFailure()
	assert.False(t, client.localAvailable(), "right after a failure, local must stay in cooldown")

	client.localSkipUntil.Store(time.Now().Add(-time.Millisecond).UnixNano())
	assert.True(t, client.localAvailable(), "an elapsed cooldown must let local be tried again")
}
