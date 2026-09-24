// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

type counterFake struct {
	ln     net.Listener
	client valkey_go.Client

	mu      sync.Mutex
	now     time.Time
	ints    map[string]int64
	expires map[string]time.Time
	incrErr bool
}

func newCounterFake(t *testing.T) *counterFake {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &counterFake{ln: ln, now: time.Unix(1_700_000_000, 0), ints: map[string]int64{}, expires: map[string]time.Time{}}
	go f.serve()
	client, err := valkey_go.NewClient(valkey_go.ClientOption{
		InitAddress:  []string{ln.Addr().String()},
		DisableCache: true,
	})
	require.NoError(t, err)
	f.client = client
	t.Cleanup(func() {
		client.Close()
		_ = ln.Close()
	})
	return f
}

func (f *counterFake) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.session(conn)
	}
}

func (f *counterFake) session(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		args, err := readLockRESPArray(r)
		if err != nil {
			return
		}
		if _, err := c.Write(f.exec(args)); err != nil {
			return
		}
	}
}

func counterFakeOK(*counterFake, respArgs) []byte { return lockSimple("OK") }

func counterFakeHello(*counterFake, respArgs) []byte { return lockErr("unknown command 'HELLO'") }

var counterFakeHandlers = map[string]func(*counterFake, respArgs) []byte{
	"HELLO":   counterFakeHello,
	"AUTH":    counterFakeOK,
	"CLIENT":  counterFakeOK,
	"SELECT":  counterFakeOK,
	"COMMAND": counterFakeOK,
	"PING":    counterFakeOK,
	"INCR":    (*counterFake).execINCR,
	"EXPIRE":  (*counterFake).execEXPIRE,
	"GET":     (*counterFake).execGET,
	"DEL":     (*counterFake).execDEL,
	"TTL":     (*counterFake).execTTL,
}

func (f *counterFake) exec(args respArgs) []byte {
	if len(args) == 0 {
		return lockErr("empty command")
	}
	cmd, rest := strings.ToUpper(args[0]), args[1:]

	f.mu.Lock()
	defer f.mu.Unlock()
	handler, ok := counterFakeHandlers[cmd]
	if !ok {
		return lockErr("unknown command '" + cmd + "'")
	}
	return handler(f, rest)
}

func (f *counterFake) execINCR(args respArgs) []byte {
	if f.incrErr {
		return lockErr("SIMULATED write failure")
	}
	key := args[0]
	f.ints[key]++
	return lockInt(f.ints[key])
}

func (f *counterFake) execEXPIRE(args respArgs) []byte {
	key := args[0]
	if _, ok := f.ints[key]; !ok {
		return lockInt(0)
	}
	f.expires[key] = f.now.Add(time.Duration(mustAtoi(args[1])) * time.Second)
	return lockInt(1)
}

func (f *counterFake) execGET(args respArgs) []byte {
	key := args[0]
	if !f.aliveLocked(key) {
		return lockNil()
	}
	return lockBulk(strconv.FormatInt(f.ints[key], 10))
}

func (f *counterFake) execDEL(args respArgs) []byte {
	deleted := int64(0)
	for _, key := range args {
		if _, ok := f.ints[key]; ok {
			deleted++
		}
		delete(f.ints, key)
		delete(f.expires, key)
	}
	return lockInt(deleted)
}

func (f *counterFake) execTTL(args respArgs) []byte {
	deadline, ok := f.expires[args[0]]
	if !ok {
		return lockInt(-1)
	}
	return lockInt(int64(deadline.Sub(f.now).Seconds()))
}

func (f *counterFake) aliveLocked(key string) bool {
	if deadline, ok := f.expires[key]; ok && !f.now.Before(deadline) {
		delete(f.ints, key)
		delete(f.expires, key)
		return false
	}
	_, ok := f.ints[key]
	return ok
}

func (f *counterFake) breakINCR() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.incrErr = true
}

func TestIncrIncrementsAndRefreshesTTL(t *testing.T) {
	f := newCounterFake(t)
	ctx := context.Background()

	n, err := Incr(ctx, f.client, "k", 48*time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	n, err = Incr(ctx, f.client, "k", 48*time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 2, n, "a second Incr adds to the same counter")

	f.mu.Lock()
	deadline := f.expires["k"]
	f.mu.Unlock()
	require.WithinDuration(t, f.now.Add(48*time.Hour), deadline, time.Second,
		"EXPIRE must re-apply on every Incr, not just the key's creation")
}

func TestIncrReportsBackendFailure(t *testing.T) {
	f := newCounterFake(t)
	f.breakINCR()

	_, err := Incr(context.Background(), f.client, "k", time.Minute)
	require.Error(t, err)
}

func TestGetIntMissReadsAsZero(t *testing.T) {
	f := newCounterFake(t)

	n, err := GetInt(context.Background(), f.client, "never-incremented")
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestGetIntReadsIncrementedValue(t *testing.T) {
	f := newCounterFake(t)
	ctx := context.Background()
	_, err := Incr(ctx, f.client, "k", time.Minute)
	require.NoError(t, err)
	_, err = Incr(ctx, f.client, "k", time.Minute)
	require.NoError(t, err)

	n, err := GetInt(ctx, f.client, "k")
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
}
