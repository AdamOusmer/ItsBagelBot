// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

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

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

var (
	respOK         = []byte("+OK\r\n")
	respOne        = []byte(":1\r\n")
	respNil        = []byte("$-1\r\n")
	respNoHello    = []byte("-ERR unknown command 'HELLO'\r\n")
	respOutage     = []byte("-ERR SIMULATED outage\r\n")
	respReadOutage = []byte("-ERR SIMULATED transient read failure\r\n")
)

type fakeValkey struct {
	ln     net.Listener
	client valkey.Client

	mu      sync.Mutex
	now     time.Time
	strs    map[string]string
	expires map[string]time.Time
	members []string
	log     [][]string
	failGET bool
	failAll bool
}

func newFakeValkey(t *testing.T) *fakeValkey {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &fakeValkey{
		ln:      ln,
		now:     time.Unix(1_700_000_000, 0),
		strs:    map[string]string{},
		expires: map[string]time.Time{},
	}
	go f.serve()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{ln.Addr().String()},
		AlwaysRESP2:       true,
		DisableCache:      true,
		ForceSingleClient: true,
	})
	require.NoError(t, err)
	f.client = client
	t.Cleanup(func() {
		client.Close()
		_ = ln.Close()
	})
	return f
}

func (f *fakeValkey) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func (f *fakeValkey) breakReads() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failGET = true
}

func (f *fakeValkey) goDown() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAll = true
}

func (f *fakeValkey) comeBack() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAll = false
}

func (f *fakeValkey) scriptRange(members []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.members = members
}

func (f *fakeValkey) commands() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.log...)
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

type respCommand []string

func (f *fakeValkey) exec(args respCommand) []byte {
	if len(args) == 0 {
		return respNoHello
	}
	switch strings.ToUpper(args[0]) {
	case "HELLO":
		return respNoHello
	case "AUTH", "CLIENT", "SELECT", "COMMAND", "PING":
		return respOK
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, args)
	if f.failAll {
		return respOutage
	}
	return f.execData(args)
}

func (f *fakeValkey) execData(c respCommand) []byte {
	switch strings.ToUpper(c[0]) {
	case "SET":
		return f.execSET(c[1:])
	case "GET":
		return f.execGET(c[1:])
	case "DEL":
		return f.execDEL(c[1:])
	case "ZRANGEBYSCORE":
		return respArray(f.members)
	}
	return respOne
}

func (f *fakeValkey) execSET(args respCommand) []byte {
	key, val := args[0], args[1]
	nx, ttl := setOptions(args[2:])
	if nx && f.aliveLocked(key) {
		return respNil
	}
	f.strs[key] = val
	delete(f.expires, key)
	if ttl > 0 {
		f.expires[key] = f.now.Add(ttl)
	}
	return respOK
}

func (f *fakeValkey) execGET(args respCommand) []byte {
	if f.failGET {
		return respReadOutage
	}
	if !f.aliveLocked(args[0]) {
		return respNil
	}
	value := f.strs[args[0]]
	return fmt.Appendf(nil, "$%d\r\n%s\r\n", len(value), value)
}

func (f *fakeValkey) execDEL(args respCommand) []byte {
	deleted := 0
	for _, key := range args {
		if _, ok := f.strs[key]; ok {
			deleted++
		}
		delete(f.strs, key)
		delete(f.expires, key)
	}
	return fmt.Appendf(nil, ":%d\r\n", deleted)
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

func setOptions(args respCommand) (bool, time.Duration) {
	nx, ttl := false, time.Duration(0)
	for i := 0; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "NX":
			nx = true
		case "EX", "PX":
			unit := time.Second
			if strings.ToUpper(args[i]) == "PX" {
				unit = time.Millisecond
			}
			n, _ := strconv.ParseInt(args[i+1], 10, 64)
			ttl, i = time.Duration(n)*unit, i+1
		}
	}
	return nx, ttl
}

func respArray(items []string) []byte {
	out := fmt.Appendf(nil, "*%d\r\n", len(items))
	for _, item := range items {
		out = fmt.Appendf(out, "$%d\r\n%s\r\n", len(item), item)
	}
	return out
}

func readRESPArray(r *bufio.Reader) ([]string, error) {
	n, err := readRESPCount(r, '*')
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		size, err := readRESPCount(r, '$')
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

func readRESPCount(r *bufio.Reader, prefix byte) (int, error) {
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
