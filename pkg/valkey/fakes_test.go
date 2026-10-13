// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

type recordingValkeyClient struct {
	valkey_go.Client
	mu       sync.Mutex
	commands []valkey_go.Completed
	batches  [][]valkey_go.Completed
	receives atomic.Int64
	closes   atomic.Int64
}

func (c *recordingValkeyClient) Do(_ context.Context, cmd valkey_go.Completed) valkey_go.ValkeyResult {
	c.mu.Lock()
	c.commands = append(c.commands, cmd)
	c.mu.Unlock()
	return valkey_go.ValkeyResult{}
}

func (c *recordingValkeyClient) DoMulti(_ context.Context, multi ...valkey_go.Completed) []valkey_go.ValkeyResult {
	c.mu.Lock()
	c.batches = append(c.batches, append([]valkey_go.Completed(nil), multi...))
	c.mu.Unlock()
	return make([]valkey_go.ValkeyResult, len(multi))
}

func (c *recordingValkeyClient) Receive(context.Context, valkey_go.Completed, func(valkey_go.PubSubMessage)) error {
	c.receives.Add(1)
	return nil
}

func (c *recordingValkeyClient) Close() {
	c.closes.Add(1)
}

type optionalReady struct {
	valkey_go.Client
	closed atomic.Bool
}

func (c *optionalReady) Close() { c.closed.Store(true) }

type (
	respArgs  []string
	respText  string
	fakeKey   string
	respToken byte
)

type scoredMember struct {
	member string
	score  int64
}

type fakeValkey struct {
	ln     net.Listener
	client valkey_go.Client

	mu        sync.Mutex
	now       time.Time
	strs      map[string]string
	expires   map[string]time.Time
	zsets     map[string][]scoredMember
	pexpireMS map[string]int64
	sets      [][]string
	broken    bool
}

func newFakeValkey(t testing.TB) *fakeValkey {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := startFakeValkey(t, ln)
	f.client = f.dial(t)
	return f
}

func startFakeValkey(t testing.TB, ln net.Listener) *fakeValkey {
	t.Helper()
	f := &fakeValkey{
		ln:        ln,
		now:       time.Unix(1_700_000_000, 0),
		strs:      map[string]string{},
		expires:   map[string]time.Time{},
		zsets:     map[string][]scoredMember{},
		pexpireMS: map[string]int64{},
	}
	t.Cleanup(func() { _ = ln.Close() })
	go f.serve()
	return f
}

func (f *fakeValkey) dial(t testing.TB) valkey_go.Client {
	t.Helper()
	client, err := valkey_go.NewClient(valkey_go.ClientOption{
		InitAddress:       []string{f.ln.Addr().String()},
		AlwaysRESP2:       true,
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func (f *fakeValkey) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func (f *fakeValkey) breakBackend() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broken = true
}

func (f *fakeValkey) put(key fakeKey, value respText) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.strs[string(key)] = string(value)
}

func (f *fakeValkey) value(key fakeKey) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.aliveLocked(string(key)) {
		return "", false
	}
	return f.strs[string(key)], true
}

func (f *fakeValkey) deadline(key fakeKey) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.expires[string(key)]
}

func (f *fakeValkey) lastSet() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sets) == 0 {
		return nil
	}
	return f.sets[len(f.sets)-1]
}

func (f *fakeValkey) zset(key fakeKey) []scoredMember {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]scoredMember(nil), f.zsets[string(key)]...)
}

func (f *fakeValkey) pexpire(key fakeKey) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pexpireMS[string(key)]
}

func (f *fakeValkey) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.session(conn)
	}
}

func (f *fakeValkey) session(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		args, err := readRESPArray(r)
		if err != nil {
			return
		}
		if _, err := c.Write(f.exec(args)); err != nil {
			return
		}
	}
}

var fakeHandshake = map[string][]byte{
	"HELLO":   respText("unknown command 'HELLO'").failure(),
	"AUTH":    respText("OK").simple(),
	"CLIENT":  respText("OK").simple(),
	"SELECT":  respText("OK").simple(),
	"COMMAND": respText("OK").simple(),
	"PING":    respText("OK").simple(),
}

var fakeCommands = map[string]func(*fakeValkey, respArgs) []byte{
	"SET":             (*fakeValkey).execSET,
	"GET":             (*fakeValkey).execGET,
	"DEL":             (*fakeValkey).execDEL,
	"EVAL":            (*fakeValkey).execEVAL,
	"INCR":            (*fakeValkey).execINCR,
	"EXPIRE":          (*fakeValkey).execEXPIRE,
	"HGET":            func(*fakeValkey, respArgs) []byte { return respNil() },
	"ZADD":            (*fakeValkey).execZADD,
	"ZREMRANGEBYRANK": (*fakeValkey).execZREMRANGEBYRANK,
	"ZCOUNT":          (*fakeValkey).execZCOUNT,
	"PEXPIRE":         (*fakeValkey).execPEXPIRE,
}

func (f *fakeValkey) exec(args respArgs) []byte {
	if len(args) == 0 {
		return respText("empty command").failure()
	}
	cmd := strings.ToUpper(args[0])
	if reply, ok := fakeHandshake[cmd]; ok {
		return reply
	}
	handler, ok := fakeCommands[cmd]
	if !ok {
		return respText(fmt.Sprintf("unknown command '%s'", cmd)).failure()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broken {
		return respText("SIMULATED write failure").failure()
	}
	return handler(f, args[1:])
}

func (f *fakeValkey) execSET(args respArgs) []byte {
	f.sets = append(f.sets, append([]string{"SET"}, args...))
	key, val := args[0], args[1]
	nx, ttl := setOptions(args[2:])
	if nx && f.aliveLocked(key) {
		return respNil()
	}
	f.strs[key] = val
	delete(f.expires, key)
	if ttl > 0 {
		f.expires[key] = f.now.Add(ttl)
	}
	return respText("OK").simple()
}

func setOptions(args respArgs) (nx bool, ttl time.Duration) {
	for i := 0; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "NX":
			nx = true
		case "EX":
			ttl, i = time.Duration(args.int(i+1))*time.Second, i+1
		case "PX":
			ttl, i = time.Duration(args.int(i+1))*time.Millisecond, i+1
		}
	}
	return nx, ttl
}

func (f *fakeValkey) execGET(args respArgs) []byte {
	if !f.aliveLocked(args[0]) {
		return respNil()
	}
	return respText(f.strs[args[0]]).bulk()
}

func (f *fakeValkey) execDEL(args respArgs) []byte {
	deleted := int64(0)
	for _, key := range args {
		if _, ok := f.strs[key]; ok {
			deleted++
		}
		delete(f.strs, key)
		delete(f.expires, key)
	}
	return respInt(deleted)
}

func (f *fakeValkey) execEVAL(args respArgs) []byte {
	if args[0] == renewIfOwner && args.int(1) == 1 {
		return f.renewLocked(args[2], args[3], time.Duration(args.int(4))*time.Millisecond)
	}
	if args[0] != releaseIfOwner || args.int(1) != 1 {
		return respText("unsupported script in fake").failure()
	}
	key, owner := args[2], args[3]
	if !f.aliveLocked(key) || f.strs[key] != owner {
		return respInt(0)
	}
	delete(f.strs, key)
	delete(f.expires, key)
	return respInt(1)
}

func (f *fakeValkey) renewLocked(key, owner string, ttl time.Duration) []byte {
	if !f.aliveLocked(key) || f.strs[key] != owner {
		return respInt(0)
	}
	f.expires[key] = f.now.Add(ttl)
	return respInt(1)
}

func (f *fakeValkey) execINCR(args respArgs) []byte {
	key := args[0]
	n := int64(0)
	if f.aliveLocked(key) {
		n = respArgs{f.strs[key]}.int(0)
	}
	n++
	f.strs[key] = strconv.FormatInt(n, 10)
	return respInt(n)
}

func (f *fakeValkey) execEXPIRE(args respArgs) []byte {
	key := args[0]
	if !f.aliveLocked(key) {
		return respInt(0)
	}
	f.expires[key] = f.now.Add(time.Duration(args.int(1)) * time.Second)
	return respInt(1)
}

func (f *fakeValkey) execZADD(args respArgs) []byte {
	key := args[0]
	f.zsets[key] = append(f.zsets[key], scoredMember{member: args[2], score: args.int(1)})
	sort.SliceStable(f.zsets[key], func(i, j int) bool { return f.zsets[key][i].score < f.zsets[key][j].score })
	return respInt(1)
}

func (f *fakeValkey) execZREMRANGEBYRANK(args respArgs) []byte {
	key, start, stop := args[0], int(args.int(1)), int(args.int(2))
	members := f.zsets[key]
	if stop < 0 {
		stop += len(members)
	}
	if start > stop || start >= len(members) {
		return respInt(0)
	}
	if stop >= len(members) {
		stop = len(members) - 1
	}
	f.zsets[key] = append(members[:start:start], members[stop+1:]...)
	return respInt(int64(stop - start + 1))
}

func (f *fakeValkey) execZCOUNT(args respArgs) []byte {
	min := args.int(1)
	n := int64(0)
	for _, member := range f.zsets[args[0]] {
		if member.score >= min {
			n++
		}
	}
	return respInt(n)
}

func (f *fakeValkey) execPEXPIRE(args respArgs) []byte {
	f.pexpireMS[args[0]] = args.int(1)
	return respInt(1)
}

func (f *fakeValkey) aliveLocked(key string) bool {
	if deadline, ok := f.expires[key]; ok && !f.now.Before(deadline) {
		delete(f.strs, key)
		delete(f.expires, key)
		return false
	}
	_, ok := f.strs[key]
	return ok
}

func (a respArgs) int(i int) int64 {
	n, _ := strconv.ParseInt(a[i], 10, 64)
	return n
}

func (t respText) simple() []byte  { return []byte("+" + string(t) + "\r\n") }
func (t respText) failure() []byte { return []byte("-ERR " + string(t) + "\r\n") }
func (t respText) bulk() []byte {
	return []byte("$" + strconv.Itoa(len(t)) + "\r\n" + string(t) + "\r\n")
}

func respInt(v int64) []byte { return []byte(":" + strconv.FormatInt(v, 10) + "\r\n") }
func respNil() []byte        { return []byte("$-1\r\n") }

func readRESPArray(r *bufio.Reader) (respArgs, error) {
	n, err := readRESPCount(r, '*')
	if err != nil {
		return nil, err
	}
	args := make(respArgs, 0, n)
	for range n {
		arg, err := readRESPBulk(r)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func readRESPBulk(r *bufio.Reader) (string, error) {
	size, err := readRESPCount(r, '$')
	if err != nil {
		return "", err
	}
	buf := make([]byte, size+2)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf[:size]), nil
}

func readRESPCount(r *bufio.Reader, prefix respToken) (int, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 || respToken(line[0]) != prefix {
		return 0, fmt.Errorf("expected %q-prefixed header, got %q", byte(prefix), line)
	}
	return strconv.Atoi(line[1:])
}
