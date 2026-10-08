// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

func counterRealValkey(t *testing.T) valkey_go.Client {
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

func TestCounterProcessorRealValkeyCompletionExpiryAndRollback(t *testing.T) {
	client := counterRealValkey(t)
	repo, raw := counterTestRepo(t)
	processor := NewCounterProcessor(repo, client)
	t.Cleanup(func() { require.NoError(t, processor.Close(context.Background())) })
	const deliveries = 24
	start := make(chan struct{})
	errs := make(chan error, deliveries)
	var wg sync.WaitGroup
	for i := range deliveries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- processor.Process(t.Context(), counterTestEvent(fmt.Sprintf("real-%d", i), 1))
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var total int64
	require.NoError(t, raw.QueryRow("SELECT value FROM counters WHERE user_id=1 AND name='deaths'").Scan(&total))
	require.EqualValues(t, deliveries, total)
	event := counterTestEvent("real-0", 1)
	keys := receiptKeys(t, client)
	require.Len(t, keys, deliveries)
	for _, key := range keys {
		value, err := client.Do(t.Context(), client.B().Get().Key(key).Build()).ToString()
		require.NoError(t, err)
		require.Equal(t, "done", value)
		ttl, err := client.Do(t.Context(), client.B().Pttl().Key(key).Build()).ToInt64()
		require.NoError(t, err)
		require.Greater(t, ttl, int64((4 * time.Minute).Milliseconds()))
		require.LessOrEqual(t, ttl, counterCompletedTTL.Milliseconds())
	}
	require.NoError(t, processor.Process(t.Context(), event))
	require.NoError(t, raw.QueryRow("SELECT value FROM counters WHERE user_id=1 AND name='deaths'").Scan(&total))
	require.EqualValues(t, deliveries, total)
	for _, key := range keys {
		require.NoError(t, client.Do(t.Context(), client.B().Eval().Script("return redis.call('pexpireat',KEYS[1],1)").Numkeys(1).Key(key).Build()).Error())
	}
	require.NoError(t, processor.Process(t.Context(), event))
	require.NoError(t, raw.QueryRow("SELECT value FROM counters WHERE user_id=1 AND name='deaths'").Scan(&total))
	require.EqualValues(t, deliveries+1, total)

	_, err := raw.Exec("CREATE TRIGGER counter_fail BEFORE INSERT ON counters BEGIN SELECT RAISE(ABORT,'temporary SQL unavailable'); END")
	require.NoError(t, err)
	failed := counterTestEvent("real-rollback", 1)
	require.Error(t, processor.Process(t.Context(), failed))
	require.Len(t, receiptKeys(t, client), 1, "a rolled-back batch must release its lease")
	_, err = raw.Exec("DROP TRIGGER counter_fail")
	require.NoError(t, err)
	require.NoError(t, processor.Process(t.Context(), failed))
	require.NoError(t, raw.QueryRow("SELECT value FROM counters WHERE user_id=1 AND name='deaths'").Scan(&total))
	require.EqualValues(t, deliveries+2, total)
	require.Len(t, receiptKeys(t, client), 2)
}

func receiptKeys(t *testing.T, client valkey_go.Client) []string {
	t.Helper()
	keys, err := client.Do(t.Context(), client.B().Keys().Pattern("loyalty:counter:receipt:1:*").Build()).AsStrSlice()
	require.NoError(t, err)
	return keys
}
