// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkeytest

import (
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

// Spawn starts a private valkey-server for this test and skips when the binary is missing.
func Spawn(t testing.TB) valkey.Client {
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
	var client valkey.Client
	require.Eventually(t, func() bool {
		client, err = valkey.NewClient(valkey.ClientOption{InitAddress: []string{address}, DisableCache: true})
		return err == nil
	}, 3*time.Second, 20*time.Millisecond)
	t.Cleanup(client.Close)
	return client
}
