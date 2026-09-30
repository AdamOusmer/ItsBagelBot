// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"bufio"
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

type recentEntry struct {
	member string
	score  int64
}

type recentFake struct {
	ln     net.Listener
	client valkey_go.Client

	mu      sync.Mutex
	zsets   map[string][]recentEntry
	pttlMS  map[string]int64
	failAll bool
}

func newRecentFake(t *testing.T) *recentFake {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &recentFake{ln: ln, zsets: map[string][]recentEntry{}, pttlMS: map[string]int64{}}
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

func (f *recentFake) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.session(conn)
	}
}

func (f *recentFake) session(c net.Conn) {
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

func recentFakeOK(*recentFake, respArgs) []byte { return lockSimple("OK") }

func recentFakeHello(*recentFake, respArgs) []byte { return lockErr("unknown command 'HELLO'") }

var recentFakeHandlers = map[string]func(*recentFake, respArgs) []byte{
	"HELLO":           recentFakeHello,
	"AUTH":            recentFakeOK,
	"CLIENT":          recentFakeOK,
	"SELECT":          recentFakeOK,
	"COMMAND":         recentFakeOK,
	"PING":            recentFakeOK,
	"ZADD":            (*recentFake).execZADD,
	"ZREMRANGEBYRANK": (*recentFake).execZREMRANGEBYRANK,
	"ZCOUNT":          (*recentFake).execZCOUNT,
	"PEXPIRE":         (*recentFake).execPEXPIRE,
}

func (f *recentFake) exec(args respArgs) []byte {
	cmd, rest := strings.ToUpper(args[0]), args[1:]
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return lockErr("SIMULATED write failure")
	}
	handler, ok := recentFakeHandlers[cmd]
	if !ok {
		return lockErr("unknown command '" + cmd + "'")
	}
	return handler(f, rest)
}

func (f *recentFake) execZADD(args respArgs) []byte {
	key := args[0]
	f.zsets[key] = append(f.zsets[key], recentEntry{member: args[2], score: mustAtoi(args[1])})
	sort.SliceStable(f.zsets[key], func(i, j int) bool { return f.zsets[key][i].score < f.zsets[key][j].score })
	return lockInt(1)
}

func (f *recentFake) execZREMRANGEBYRANK(args respArgs) []byte {
	key, start, stop := args[0], int(mustAtoi(args[1])), int(mustAtoi(args[2]))
	entries := f.zsets[key]
	if stop < 0 {
		stop += len(entries)
	}
	if start > stop || start >= len(entries) {
		return lockInt(0)
	}
	if stop >= len(entries) {
		stop = len(entries) - 1
	}
	f.zsets[key] = append(entries[:start:start], entries[stop+1:]...)
	return lockInt(int64(stop - start + 1))
}

func (f *recentFake) execZCOUNT(args respArgs) []byte {
	min := mustAtoi(args[1])
	n := int64(0)
	for _, e := range f.zsets[args[0]] {
		if e.score >= min {
			n++
		}
	}
	return lockInt(n)
}

func (f *recentFake) execPEXPIRE(args respArgs) []byte {
	f.pttlMS[args[0]] = mustAtoi(args[1])
	return lockInt(1)
}

func TestRecordRecentThenCountSinceSeesOnlyLinesInsideTheWindow(t *testing.T) {
	f := newRecentFake(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	log := RecentLog{Key: "k", Keep: 100, TTL: time.Hour}

	require.NoError(t, RecordRecent(ctx, f.client, log, now.Add(-10*time.Minute)))
	require.NoError(t, RecordRecent(ctx, f.client, log, now.Add(-2*time.Minute)))
	require.NoError(t, RecordRecent(ctx, f.client, log, now))

	n, err := CountRecentSince(ctx, f.client, "k", now.Add(-5*time.Minute))
	require.NoError(t, err)
	require.EqualValues(t, 2, n, "a line older than the window must not count")
}

func TestRecordRecentKeepsSameMillisecondLinesDistinct(t *testing.T) {
	f := newRecentFake(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	log := RecentLog{Key: "k", Keep: 100, TTL: time.Hour}

	for range 3 {
		require.NoError(t, RecordRecent(ctx, f.client, log, now))
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	members := map[string]struct{}{}
	for _, e := range f.zsets["k"] {
		members[e.member] = struct{}{}
	}
	require.Len(t, members, 3)
}

func TestRecordRecentTrimsToNewestKeepAndSetsTTL(t *testing.T) {
	f := newRecentFake(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	log := RecentLog{Key: "k", Keep: 100, TTL: time.Hour}

	for i := range 130 {
		require.NoError(t, RecordRecent(ctx, f.client, log, now.Add(time.Duration(i)*time.Second)))
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	require.Len(t, f.zsets["k"], 100)
	require.EqualValues(t, now.Add(30*time.Second).UnixMilli(), f.zsets["k"][0].score, "trim must drop the oldest entries")
	require.EqualValues(t, time.Hour.Milliseconds(), f.pttlMS["k"])
}

func TestRecordRecentReportsBackendFailure(t *testing.T) {
	f := newRecentFake(t)
	f.mu.Lock()
	f.failAll = true
	f.mu.Unlock()

	err := RecordRecent(context.Background(), f.client, RecentLog{Key: "k", Keep: 100, TTL: time.Hour}, time.Now())
	require.Error(t, err)
}

func TestCountRecentSinceMissingKeyReadsAsZero(t *testing.T) {
	f := newRecentFake(t)

	n, err := CountRecentSince(context.Background(), f.client, "absent", time.Unix(0, 0))
	require.NoError(t, err)
	require.Zero(t, n)
}
