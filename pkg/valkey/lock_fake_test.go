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

type lockFake struct {
	ln     net.Listener
	client valkey_go.Client

	mu      sync.Mutex
	now     time.Time
	strs    map[string]string
	expires map[string]time.Time
	failSET bool
}

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
		DisableCache: true,
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

func (f *lockFake) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func (f *lockFake) breakSET() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failSET = true
}

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

type respArgs []string

func (f *lockFake) exec(args respArgs) []byte {
	if len(args) == 0 {
		return lockErr("empty command")
	}
	cmd, rest := strings.ToUpper(args[0]), args[1:]

	f.mu.Lock()
	defer f.mu.Unlock()
	switch cmd {
	case "HELLO":
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

func (f *lockFake) execSET(args respArgs) []byte {
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

func (f *lockFake) execGET(args respArgs) []byte {
	if !f.aliveLocked(args[0]) {
		return lockNil()
	}
	return lockBulk(f.strs[args[0]])
}

func (f *lockFake) execDEL(args respArgs) []byte {
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

func (f *lockFake) execEVAL(args respArgs) []byte {
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

func setOptions(args respArgs) (bool, time.Duration) {
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

func lockSimple(s string) []byte { return []byte("+" + s + "\r\n") }
func lockInt(v int64) []byte     { return []byte(":" + strconv.FormatInt(v, 10) + "\r\n") }
func lockBulk(s string) []byte   { return []byte("$" + strconv.Itoa(len(s)) + "\r\n" + s + "\r\n") }
func lockNil() []byte            { return []byte("$-1\r\n") }
func lockErr(s string) []byte    { return []byte("-ERR " + s + "\r\n") }

func readLockRESPArray(r *bufio.Reader) (respArgs, error) {
	n, err := readLockCount(r, '*')
	if err != nil {
		return nil, err
	}
	args := make(respArgs, 0, n)
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
	buf := make([]byte, size+2)
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
