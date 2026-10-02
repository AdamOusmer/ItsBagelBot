// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type fakeStore struct {
	mu       sync.Mutex
	claims   map[string]bool
	seenN    int
	releaseN int
	err      error
}

func newFakeStore() *fakeStore { return &fakeStore{claims: map[string]bool{}} }

func (f *fakeStore) Seen(_ context.Context, key string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenN++
	if f.err != nil {
		return false, f.err
	}
	seen := f.claims[key]
	f.claims[key] = true
	return seen, nil
}

func (f *fakeStore) Release(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseN++
	delete(f.claims, key)
	return nil
}

func (f *fakeStore) seenCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seenN
}

type fakeValkey struct {
	client valkey.Client

	mu     sync.Mutex
	keys   map[string]bool
	lastPX string
	broken bool
}

func newFakeValkey(t *testing.T) *fakeValkey {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &fakeValkey{keys: map[string]bool{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{ln.Addr().String()}, AlwaysRESP2: true, DisableCache: true, ForceSingleClient: true,
	})
	require.NoError(t, err)
	f.client = client
	t.Cleanup(func() {
		client.Close()
		_ = ln.Close()
	})
	return f
}

func (f *fakeValkey) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		args, err := readCommand(r)
		if err != nil {
			return
		}
		if _, err := io.WriteString(conn, f.reply(args)); err != nil {
			return
		}
	}
}

func (f *fakeValkey) reply(args []string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch strings.ToUpper(args[0]) {
	case "HELLO":
		return "-ERR unknown command 'HELLO'\r\n"
	case "SET":
		return f.set(args)
	case "DEL":
		delete(f.keys, args[1])
		return ":1\r\n"
	}
	return "+OK\r\n"
}

func (f *fakeValkey) set(args []string) string {
	if f.broken {
		return "-ERR SIMULATED outage\r\n"
	}
	f.lastPX = args[len(args)-1]
	if f.keys[args[1]] {
		return "$-1\r\n"
	}
	f.keys[args[1]] = true
	return "+OK\r\n"
}

func (f *fakeValkey) breakServer() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broken = true
}

func (f *fakeValkey) lastSetPX() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastPX
}

func readCommand(r *bufio.Reader) ([]string, error) {
	var count int
	if _, err := fmt.Fscanf(r, "*%d\r\n", &count); err != nil {
		return nil, err
	}
	args := make([]string, count)
	for i := range args {
		var size int
		if _, err := fmt.Fscanf(r, "$%d\r\n", &size); err != nil {
			return nil, err
		}
		arg := make([]byte, size+2)
		if _, err := io.ReadFull(r, arg); err != nil {
			return nil, err
		}
		args[i] = string(arg[:size])
	}
	return args, nil
}
