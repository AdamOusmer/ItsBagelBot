// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"ItsBagelBot/internal/domain/event/data"
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

type counterFakeValkey struct {
	valkey_go.Client
	mu           sync.Mutex
	listener     net.Listener
	values       map[string]string
	expires      map[string]time.Time
	now          time.Time
	batches      [][]string
	failClaim    bool
	failComplete bool
}

func newCounterFakeValkey(t *testing.T) *counterFakeValkey {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &counterFakeValkey{listener: listener, values: map[string]string{}, expires: map[string]time.Time{}, now: time.Now()}
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go f.session(c)
		}
	}()
	client, err := valkey_go.NewClient(valkey_go.ClientOption{InitAddress: []string{listener.Addr().String()}, DisableCache: true, ForceSingleClient: true})
	require.NoError(t, err)
	f.Client = client
	t.Cleanup(func() { client.Close(); _ = listener.Close() })
	return f
}

func (f *counterFakeValkey) Do(_ context.Context, _ valkey_go.Completed) valkey_go.ValkeyResult {
	panic("counter path must use explicit DoMulti")
}

func (f *counterFakeValkey) DoMulti(ctx context.Context, commands ...valkey_go.Completed) []valkey_go.ValkeyResult {
	scripts := make([]string, len(commands))
	for i, command := range commands {
		scripts[i] = command.Commands()[1]
	}
	f.mu.Lock()
	f.batches = append(f.batches, scripts)
	f.mu.Unlock()
	return f.Client.DoMulti(ctx, commands...)
}

func (f *counterFakeValkey) session(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		args, err := readCounterRESPCommand(r)
		if err != nil {
			return
		}
		if _, err = c.Write(f.execute(args)); err != nil {
			return
		}
	}
}

func readCounterRESPCommand(r *bufio.Reader) ([]string, error) {
	n, err := readCounterRESPLength(r, "*")
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("empty RESP command")
	}
	args := make([]string, n)
	for i := range args {
		args[i], err = readCounterRESPBulkString(r)
		if err != nil {
			return nil, err
		}
	}
	return args, nil
}

func readCounterRESPBulkString(r *bufio.Reader) (string, error) {
	size, err := readCounterRESPLength(r, "$")
	if err != nil {
		return "", err
	}
	body := make([]byte, size+2)
	if _, err := io.ReadFull(r, body); err != nil {
		return "", err
	}
	return string(body[:size]), nil
}

func readCounterRESPLength(r *bufio.Reader, prefix string) (int, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, err
	}
	size, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
	if err != nil {
		return 0, err
	}
	if size < 0 {
		return 0, fmt.Errorf("negative RESP length: %d", size)
	}
	return size, nil
}

func (f *counterFakeValkey) execute(args []string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch strings.ToUpper(args[0]) {
	case "HELLO":
		return []byte("-ERR unknown command 'HELLO'\r\n")
	case "CLIENT", "AUTH", "SELECT", "COMMAND", "PING":
		return []byte("+OK\r\n")
	case "EVAL":
		return f.executeScript(args)
	default:
		return []byte("-ERR unsupported command\r\n")
	}
}

type counterTestLease struct {
	key   string
	owner string
	ttl   time.Duration
}

func counterTestScriptLease(args []string) counterTestLease {
	lease := counterTestLease{key: args[3], owner: args[4]}
	if len(args) > 5 {
		milliseconds, _ := strconv.ParseInt(args[5], 10, 64)
		lease.ttl = time.Duration(milliseconds) * time.Millisecond
	}
	return lease
}

func (f *counterFakeValkey) executeScript(args []string) []byte {
	lease := counterTestScriptLease(args)
	f.expireReceipt(lease.key)
	switch args[1] {
	case counterClaimScript:
		return f.claimReceipt(lease)
	case counterCompleteScript:
		return f.completeReceipt(lease)
	case counterReleaseScript:
		return f.releaseReceipt(lease)
	default:
		return []byte("-ERR unsupported command\r\n")
	}
}

func (f *counterFakeValkey) claimReceipt(lease counterTestLease) []byte {
	if f.failClaim {
		return []byte("-ERR unavailable\r\n")
	}
	if f.values[lease.key] == "done" {
		return []byte(":2\r\n")
	}
	if f.values[lease.key] != "" {
		return []byte(":0\r\n")
	}
	f.storeReceipt(lease, lease.owner)
	return []byte(":1\r\n")
}

func (f *counterFakeValkey) completeReceipt(lease counterTestLease) []byte {
	if f.failComplete {
		return []byte("-ERR unavailable\r\n")
	}
	if f.values[lease.key] != lease.owner {
		return []byte(":0\r\n")
	}
	f.storeReceipt(lease, "done")
	return []byte(":1\r\n")
}

func (f *counterFakeValkey) releaseReceipt(lease counterTestLease) []byte {
	if f.values[lease.key] != lease.owner {
		return []byte(":0\r\n")
	}
	f.deleteReceipt(lease.key)
	return []byte(":1\r\n")
}

func (f *counterFakeValkey) storeReceipt(lease counterTestLease, value string) {
	f.values[lease.key] = value
	f.expires[lease.key] = f.now.Add(lease.ttl)
}

func (f *counterFakeValkey) expireReceipt(key string) {
	if expiry, ok := f.expires[key]; ok && !expiry.After(f.now) {
		f.deleteReceipt(key)
	}
}

func (f *counterFakeValkey) deleteReceipt(key string) {
	delete(f.values, key)
	delete(f.expires, key)
}

func (f *counterFakeValkey) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func (f *counterFakeValkey) hold(dto data.CounterBumpedDTO, owner string, ttl time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := counterReceiptKey(dto)
	f.values[key] = owner
	f.expires[key] = f.now.Add(ttl)
}

func (f *counterFakeValkey) holder(dto data.CounterBumpedDTO) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.values[counterReceiptKey(dto)]
}

func (f *counterFakeValkey) ttl(dto data.CounterBumpedDTO) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.expires[counterReceiptKey(dto)].Sub(f.now)
}

func (f *counterFakeValkey) outage(claim, complete bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failClaim, f.failComplete = claim, complete
}
