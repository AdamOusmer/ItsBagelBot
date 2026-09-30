// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build linux || darwin

package db

import (
	"context"
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func socketOption(t *testing.T, conn *net.TCPConn, level, option int) int {
	t.Helper()

	raw, err := conn.SyscallConn()
	require.NoError(t, err)
	var value int
	var sockErr error
	require.NoError(t, raw.Control(func(fd uintptr) {
		value, sockErr = syscall.GetsockoptInt(int(fd), level, option)
	}))
	require.NoError(t, sockErr)
	return value
}

func TestRetryDialerAppliesTCPKeepAlive(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	conn, err := newRetryDialer().DialContext(context.Background(), "tcp", listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	tcp := conn.(*net.TCPConn)
	require.NoError(t, tcp.SetKeepAlive(true))

	require.NotZero(t, socketOption(t, tcp, syscall.SOL_SOCKET, syscall.SO_KEEPALIVE))
	require.Equal(t, 5, socketOption(t, tcp, syscall.IPPROTO_TCP, syscall.TCP_KEEPINTVL))
	require.Equal(t, 4, socketOption(t, tcp, syscall.IPPROTO_TCP, syscall.TCP_KEEPCNT))
}
