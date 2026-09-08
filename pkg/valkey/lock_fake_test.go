// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// A stateful in-process fake Valkey speaking RESP2, just wide enough for the
// lock idioms: SET with NX/EX/PX, GET, DEL and the one compare-and-delete
// script. The real valkey-go client dials it, so the tests exercise the actual
// wire encoding and, crucially, the real client's Nil-error surfacing of a
// declined NX — the case wonClaim exists to split.
//
// Why not the recording stub in client_fake_test.go: it answers every command
// with a zero ValkeyResult, so it cannot express "the key already exists".
// Exclusion is decided from SERVER state, so the double must remember keys and
// expiries. Why not reuse internal/projection's fake: it is an unexported test
// helper in another package, built around hashes and a command surface this
// package does not issue. Why not miniredis: not a dependency here, and this
// is five verbs.
//
// The clock is injected (advance) rather than slept on: expiry is the property
// under test in two cases, and a test that sleeps a real TTL is both slow and
// flaky on a loaded machine.
type lockFake struct {
	ln     net.Listener
	client valkey_go.Client

	mu      sync.Mutex
	now     time.Time
	strs    map[string]string
	expires map[string]time.Time
	failSET bool
}

// newLockFake boots the listener and a client pointed at it. Cleanup is
// registered on t.
func newLockFake(t *testing.T) *lockFake {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake valkey listen: %v", err)
	}
	f := &lockFake{
		ln:      ln,
		now:     time.Unix(1_700_000_000, 0),
		strs:    map[string]string{},
		expires: map[string]time.Time{},
	}
	go f.serve()
	client, err := valkey_go.NewClient(valkey_go.ClientOption{
		InitAddress:  []string{ln.Addr().String()},
		DisableCache: true, // no CLIENT TRACKING init; the fake speaks plain RESP2
	})
	if err != nil {
		t.Fatalf("fake valkey client: %v", err)
	}
	f.client = client
	t.Cleanup(func() {
		client.Close()
		_ = ln.Close()
	})
	return f
}

// advance moves the fake's clock, expiring whatever that passes.
func (f *lockFake) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// breakSET makes every following SET answer with a server error, so a test can
// prove a backend failure is reported rather than read as a lost race.
func (f *lockFake) breakSET() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failSET = true
}

// value returns a key's stored value, honouring expiry.
func (f *lockFake) value(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.aliveLocked(key) {
		return "", false
	}
	return f.strs[key], true
}

func (f *lockFake) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.session(conn)
	}
}

func (f *lockFake) session(c net.Conn) {
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

// exec runs one command under the fake's lock.
func (f *lockFake) exec(args []string) []byte {
	if len(args) == 0 {
		return lockErr("empty command")
	}
	cmd, rest := strings.ToUpper(args[0]), args[1:]

	f.mu.Lock()
	defer f.mu.Unlock()
	switch cmd {
	case "HELLO":
		// Match valkey-go's noHello regex so it falls back to RESP2.
		return lockErr("unknown command 'HELLO'")
	case "AUTH", "CLIENT", "SELECT", "COMMAND", "PING":
		return lockSimple("OK")
	case "SET":
		return f.execSET(rest)
	case "GET":
		return f.execGET(rest)
	case "DEL":
		return f.execDEL(rest)
	case "EVAL":
		return f.execEVAL(rest)
	}
	return lockErr(fmt.Sprintf("unknown command '%s'", cmd))
}

func (f *lockFake) execSET(args []string) []byte {
	if f.failSET {
		return lockErr("SIMULATED write failure")
	}
	key, val := args[0], args[1]
	nx, ttl := setOptions(args[2:])
	if nx && f.aliveLocked(key) {
		return lockNil()
	}
	f.strs[key] = val
	delete(f.expires, key)
	if ttl > 0 {
		f.expires[key] = f.now.Add(ttl)
	}
	return lockSimple("OK")
}

func (f *lockFake) execGET(args []string) []byte {
	if !f.aliveLocked(args[0]) {
		return lockNil()
	}
	return lockBulk(f.strs[args[0]])
}

func (f *lockFake) execDEL(args []string) []byte {
	deleted := int64(0)
	for _, key := range args {
		if _, ok := f.strs[key]; ok {
			deleted++
		}
		delete(f.strs, key)
		delete(f.expires, key)
	}
	return lockInt(deleted)
}

// execEVAL implements only releaseIfOwner: DEL KEYS[1] while its value equals
// ARGV[1]. Any other script is an error, so a caller that grows a second
// script cannot silently get this one's behaviour.
func (f *lockFake) execEVAL(args []string) []byte {
	script, numkeys := args[0], mustAtoi(args[1])
	if script != releaseIfOwner || numkeys != 1 {
		return lockErr("unsupported script in fake")
	}
	key, owner := args[2], args[3]
	if !f.aliveLocked(key) || f.strs[key] != owner {
		return lockInt(0)
	}
	delete(f.strs, key)
	delete(f.expires, key)
	return lockInt(1)
}

// setOptions decodes the SET flags this fake understands. An unknown flag is
// ignored rather than rejected: the tests assert on outcomes, and a flag that
// changed nothing here would fail its own assertion anyway.
func setOptions(args []string) (bool, time.Duration) {
	nx, ttl := false, time.Duration(0)
	for i := 0; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "NX":
			nx = true
		case "EX":
			ttl, i = time.Duration(mustAtoi(args[i+1]))*time.Second, i+1
		case "PX":
			ttl, i = time.Duration(mustAtoi(args[i+1]))*time.Millisecond, i+1
		}
	}
	return nx, ttl
}

// aliveLocked applies lazy expiry; the caller holds mu.
func (f *lockFake) aliveLocked(key string) bool {
	if deadline, ok := f.expires[key]; ok && !f.now.Before(deadline) {
		delete(f.strs, key)
		delete(f.expires, key)
		return false
	}
	_, ok := f.strs[key]
	return ok
}

func mustAtoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// --- RESP2 wire helpers ---

func lockSimple(s string) []byte { return []byte("+" + s + "\r\n") }
func lockInt(v int64) []byte     { return []byte(":" + strconv.FormatInt(v, 10) + "\r\n") }
func lockBulk(s string) []byte   { return []byte("$" + strconv.Itoa(len(s)) + "\r\n" + s + "\r\n") }
func lockNil() []byte            { return []byte("$-1\r\n") }
func lockErr(s string) []byte    { return []byte("-ERR " + s + "\r\n") }

func readLockRESPArray(r *bufio.Reader) ([]string, error) {
	n, err := readLockCount(r, '*')
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		arg, err := readLockBulk(r)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func readLockBulk(r *bufio.Reader) (string, error) {
	size, err := readLockCount(r, '$')
	if err != nil {
		return "", err
	}
	buf := make([]byte, size+2) // payload + CRLF
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf[:size]), nil
}

func readLockCount(r *bufio.Reader, prefix byte) (int, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 || line[0] != prefix {
		return 0, fmt.Errorf("expected %q-prefixed header, got %q", prefix, line)
	}
	return strconv.Atoi(line[1:])
}
