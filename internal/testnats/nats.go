// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package testnats

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

// Connect starts an isolated NATS server and connection, cleaned up with the test.
func Connect(t testing.TB) *nats.Conn {
	t.Helper()
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	s.Start()
	t.Cleanup(func() {
		s.Shutdown()
		s.WaitForShutdown()
	})
	require.True(t, s.ReadyForConnections(5*time.Second), "NATS server did not become ready")
	nc, err := nats.Connect(s.ClientURL(), nats.NoReconnect())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	return nc
}
