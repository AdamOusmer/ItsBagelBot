// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

// kvServer is a real TCP server speaking the sliver of RESP these helpers
// use, driving a real valkey-go client. The helpers are tested through the
// wire rather than against a fake client because the whole point of KV is the
// command it builds: a fake whose B() we would have to supply ourselves
// (valkey-go's builder is internal) could only ever echo back what the test
// already assumed.
type kvServer struct {
	ln net.Listener

	mu     sync.Mutex
	values map[string]string
	sets   [][]string
}

func newKVServer(tb testing.TB) *kvServer {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = ln.Close() })
	s := &kvServer{ln: ln, values: map[string]string{}}
	go s.serve()
	return s
}

func (s *kvServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *kvServer) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		args, err := readRESPCommand(r)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		if _, err := io.WriteString(c, s.reply(args)); err != nil {
			return
		}
	}
}

// reply answers one command. HELLO is refused so the client falls back to
// RESP2, matching what the other scripted-server tests in this repo do.
func (s *kvServer) reply(args []string) string {
	switch strings.ToUpper(args[0]) {
	case "HELLO":
		return "-ERR unknown command 'HELLO'\r\n"
	case "GET":
		return s.get(args[1])
	case "SET":
		s.set(args)
		return "+OK\r\n"
	case "DEL":
		s.del(args[1])
		return ":1\r\n"
	default:
		return ":1\r\n"
	}
}

func (s *kvServer) get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := s.values[key]
	if !ok {
		return "$-1\r\n"
	}
	return fmt.Sprintf("$%d\r\n%s\r\n", len(got), got)
}

func (s *kvServer) set(args []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[args[1]] = args[2]
	s.sets = append(s.sets, append([]string(nil), args...))
}

func (s *kvServer) del(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
}

// lastSet returns the argument vector of the most recent SET.
func (s *kvServer) lastSet() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sets) == 0 {
		return nil
	}
	return s.sets[len(s.sets)-1]
}

func (s *kvServer) put(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
}

// readRESPCommand reads one inbound array-of-bulk-strings command.
func readRESPCommand(r *bufio.Reader) ([]string, error) {
	header, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(header, "*")))
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, count)
	for range count {
		arg, err := readRESPBulk(r)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func readRESPBulk(r *bufio.Reader) (string, error) {
	sizeLine, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	size, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(sizeLine, "$")))
	if err != nil {
		return "", err
	}
	buf := make([]byte, size+2) // payload + CRLF
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf[:size]), nil
}

func dialKV(tb testing.TB, s *kvServer) KV {
	tb.Helper()
	client, err := valkey_go.NewClient(valkey_go.ClientOption{
		InitAddress:       []string{s.ln.Addr().String()},
		AlwaysRESP2:       true,
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(tb, err)
	tb.Cleanup(client.Close)
	return NewKV(client)
}

type kvPayload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestKVStringRoundTripCarriesTTL(t *testing.T) {
	server := newKVServer(t)
	kv := dialKV(t, server)
	ctx := context.Background()

	require.NoError(t, kv.Set(ctx, Key{Name: "k", TTL: time.Minute}, "v"))
	got, ok := kv.GetString(ctx, "k")

	assert.True(t, ok)
	assert.Equal(t, "v", got)
	assert.Equal(t, []string{"SET", "k", "v", "EX", "60"}, server.lastSet())
}

// TestKVSetWithoutTTLIsPersistent pins the zero-TTL branch: the flags that
// must outlive the process (the reauth marker, the gateway status) are
// written through the same Set, and an accidental EX 0 would be rejected by
// Valkey rather than silently persisting.
func TestKVSetWithoutTTLIsPersistent(t *testing.T) {
	server := newKVServer(t)
	kv := dialKV(t, server)

	require.NoError(t, kv.Set(context.Background(), Key{Name: "flag"}, "1"))

	assert.Equal(t, []string{"SET", "flag", "1"}, server.lastSet())
}

func TestKVGetStringMissAndDelete(t *testing.T) {
	server := newKVServer(t)
	kv := dialKV(t, server)
	ctx := context.Background()

	_, missing := kv.GetString(ctx, "absent")
	server.put("empty", "")
	_, stored := kv.GetString(ctx, "empty")

	require.NoError(t, kv.Set(ctx, Key{Name: "doomed"}, "v"))
	require.NoError(t, kv.Del(ctx, "doomed"))
	_, afterDelete := kv.GetString(ctx, "doomed")

	assert.False(t, missing)
	assert.False(t, stored, "a stored empty string is indistinguishable from a miss")
	assert.False(t, afterDelete)
}

func TestKVJSONRoundTripAndUndecodableValue(t *testing.T) {
	server := newKVServer(t)
	kv := dialKV(t, server)
	ctx := context.Background()

	require.NoError(t, SetJSON(ctx, kv, Key{Name: "blob", TTL: time.Hour}, kvPayload{Name: "desk", Count: 3}))
	got, ok := GetJSON[kvPayload](ctx, kv, "blob")

	server.put("garbage", "{not json")
	fallback, decoded := GetJSON[kvPayload](ctx, kv, "garbage")

	assert.True(t, ok)
	assert.Equal(t, kvPayload{Name: "desk", Count: 3}, got)
	assert.False(t, decoded, "a blob from a build that no longer exists reads as a miss")
	assert.Equal(t, kvPayload{}, fallback)
}
