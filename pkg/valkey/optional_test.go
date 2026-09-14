// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	vk "github.com/valkey-io/valkey-go"
)

func TestOptionalClientFailsPromptlyWhileDisconnected(t *testing.T) {
	c := newOptionalClient(func() (vk.Client, error) { return nil, errOptionalUnavailable })
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.ErrorIs(t, c.Do(ctx, c.B().Get().Key("test").Build()).Error(), errOptionalUnavailable)
	results := c.DoMulti(ctx, c.B().Get().Key("one").Build(), c.B().Get().Key("two").Build())
	require.Len(t, results, 2)
	require.ErrorIs(t, results[0].Error(), errOptionalUnavailable)
}

type optionalReady struct {
	vk.Client
	closed atomic.Bool
}

func (c *optionalReady) Close() { c.closed.Store(true) }

func TestOptionalClientPublishesConnectionAndClosesIt(t *testing.T) {
	ready := &optionalReady{}
	c := newOptionalClient(func() (vk.Client, error) { return ready, nil })
	require.Eventually(t, func() bool { return c.current() == ready }, time.Second, time.Millisecond)
	c.Close()
	c.Close()
	require.True(t, ready.closed.Load())
}
