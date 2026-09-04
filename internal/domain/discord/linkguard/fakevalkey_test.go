// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// fakeValkey is a minimal in-process RESP2 server that the real valkey-go
// client dials, so Guarder tests exercise genuine wire round trips instead
// of mocked results. Shaped after internal/projection/fakevalkey_test.go's
// fake, trimmed to the handful of commands this package issues (SADD,
// SCARD, EXPIRE with NX, HSET, HGET, DEL, EXISTS) plus a controllable clock
// so Window/CorroborationWindow/FleetTTL expiry can be exercised without a
// real sleep.
type fakeValkey struct {
	t       *testing.T
	ln      net.Listener
	addr    string
	client  valkey_go.Client
	nowFunc func() time.Time

	mu      sync.Mutex
	sets    map[string]map[string]bool
	hashes  map[string]map[string]string
	expires map[string]time.Time
	log     []fakeOp
}

type fakeOp struct {
	cmd  string
	args []string
}

// newFakeValkey boots the listener + client. Caller must Close (registered
// via t.Cleanup).
func newFakeValkey(t *testing.T) *fakeValkey {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake valkey listen: %v", err)
	}
	f := &fakeValkey{
		t:       t,
		ln:      ln,
		addr:    ln.Addr().String(),
		nowFunc: time.Now,
		sets:    map[string]map[string]bool{},
		hashes:  map[string]map[string]string{},
		expires: map[string]time.Time{},
	}
	go f.serve()
	client, err := valkey_go.NewClient(valkey_go.ClientOption{
		InitAddress:  []string{f.addr},
		DisableCache: true, // the fake speaks plain RESP2, no CLIENT TRACKING
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

// advance moves the fake's clock forward, so tests can cross a Window,
// CorroborationWindow, or FleetTTL boundary deterministically.
func (f *fakeValkey) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.nowFunc()
	f.nowFunc = func() time.Time { return now.Add(d) }
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

var fakeCommandHandlers = map[string]func(*fakeValkey, []string) []byte{
	"SADD":   (*fakeValkey).execSADD,
	"SCARD":  (*fakeValkey).execSCARD,
	"EXPIRE": (*fakeValkey).execEXPIRE,
	"HSET":   (*fakeValkey).execHSET,
	"HGET":   (*fakeValkey).execHGET,
	"DEL":    (*fakeValkey).execDEL,
	"EXISTS": (*fakeValkey).execEXISTS,
}

// exec runs one command atomically under the global fake lock and records
// it, matching how the real server serializes one connection's commands.
func (f *fakeValkey) exec(args []string) []byte {
	if len(args) == 0 {
		return respError("empty command")
	}
	cmd := strings.ToUpper(args[0])
	rest := args[1:]

	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, fakeOp{cmd: cmd, args: rest})

	switch cmd {
	case "HELLO":
		// Matches valkey-go's RESP2 fallback probe.
		return respError("unknown command 'HELLO'")
	case "AUTH", "CLIENT", "SELECT", "COMMAND", "PING":
		return respSimple("OK")
	}
	if h, ok := fakeCommandHandlers[cmd]; ok {
		return h(f, rest)
	}
	return respError(fmt.Sprintf("unknown command '%s'", cmd))
}

func (f *fakeValkey) execSADD(args []string) []byte {
	key := args[0]
	f.aliveLocked(key) // lazy-expire before writing, so a stale set does not linger
	if f.sets[key] == nil {
		f.sets[key] = map[string]bool{}
	}
	added := int64(0)
	for _, m := range args[1:] {
		if !f.sets[key][m] {
			f.sets[key][m] = true
			added++
		}
	}
	return respInt(added)
}

func (f *fakeValkey) execSCARD(args []string) []byte {
	key := args[0]
	if !f.aliveLocked(key) {
		return respInt(0)
	}
	return respInt(int64(len(f.sets[key])))
}

func (f *fakeValkey) execEXPIRE(args []string) []byte {
	key := args[0]
	secs, _ := strconv.Atoi(args[1])
	mode := ""
	if len(args) > 2 {
		mode = strings.ToUpper(args[2])
	}
	target := f.nowFunc().Add(time.Duration(secs) * time.Second)
	f.aliveLocked(key) // lazy-expire first so NX sees a truthful "has expiry" state
	current, hasCurrent := f.expires[key]
	switch mode {
	case "NX":
		if hasCurrent {
			return respInt(0)
		}
	case "GT":
		if !hasCurrent || !target.After(current) {
			return respInt(0)
		}
	}
	f.expires[key] = target
	return respInt(1)
}

func (f *fakeValkey) execHSET(args []string) []byte {
	key := args[0]
	pairs := args[1:]
	if len(pairs)%2 != 0 {
		return respError("wrong number of arguments for HSET")
	}
	f.aliveLocked(key)
	if f.hashes[key] == nil {
		f.hashes[key] = map[string]string{}
	}
	added := int64(0)
	for i := 0; i < len(pairs); i += 2 {
		if _, exists := f.hashes[key][pairs[i]]; !exists {
			added++
		}
		f.hashes[key][pairs[i]] = pairs[i+1]
	}
	return respInt(added)
}

func (f *fakeValkey) execHGET(args []string) []byte {
	key, field := args[0], args[1]
	if !f.aliveLocked(key) {
		return respNil()
	}
	v, ok := f.hashes[key][field]
	if !ok {
		return respNil()
	}
	return respBulk(v)
}

func (f *fakeValkey) execDEL(args []string) []byte {
	deleted := int64(0)
	for _, key := range args {
		if _, ok := f.sets[key]; ok {
			delete(f.sets, key)
			deleted++
		}
		if _, ok := f.hashes[key]; ok {
			delete(f.hashes, key)
			deleted++
		}
		delete(f.expires, key)
	}
	return respInt(deleted)
}

func (f *fakeValkey) execEXISTS(args []string) []byte {
	n := int64(0)
	for _, key := range args {
		if f.aliveLocked(key) {
			n++
		}
	}
	return respInt(n)
}

// aliveLocked applies lazy expiry and reports whether key still holds data.
// Caller holds mu.
func (f *fakeValkey) aliveLocked(key string) bool {
	if dl, ok := f.expires[key]; ok {
		if !f.nowFunc().Before(dl) {
			delete(f.sets, key)
			delete(f.hashes, key)
			delete(f.expires, key)
			return false
		}
	}
	_, hasSet := f.sets[key]
	_, hasHash := f.hashes[key]
	return hasSet || hasHash
}
