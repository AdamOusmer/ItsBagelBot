// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bustest

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func NATS(t *testing.T) *nats.Conn {
	t.Helper()
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(s.Shutdown)

	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	return nc
}

func Respond(t *testing.T, nc *nats.Conn, subject string, replies ...string) *atomic.Int32 {
	t.Helper()
	var served atomic.Int32
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		i := int(served.Add(1)) - 1
		if i >= len(replies) {
			i = len(replies) - 1
		}
		_ = msg.Respond([]byte(replies[i]))
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return &served
}
