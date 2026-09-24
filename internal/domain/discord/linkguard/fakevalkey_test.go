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

type wireArgs []string

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
		if _, err := c.Write(f.exec(wireArgs(args))); err != nil {
			return
		}
	}
}

var fakeCommandHandlers = map[string]func(*fakeValkey, wireArgs) []byte{
	"SADD":   (*fakeValkey).execSADD,
	"SCARD":  (*fakeValkey).execSCARD,
	"EXPIRE": (*fakeValkey).execEXPIRE,
	"HSET":   (*fakeValkey).execHSET,
	"HGET":   (*fakeValkey).execHGET,
	"DEL":    (*fakeValkey).execDEL,
	"EXISTS": (*fakeValkey).execEXISTS,
}

func (f *fakeValkey) exec(args wireArgs) []byte {
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
		return respError("unknown command 'HELLO'")
	case "AUTH", "CLIENT", "SELECT", "COMMAND", "PING":
		return respSimple("OK")
	}
	if h, ok := fakeCommandHandlers[cmd]; ok {
		return h(f, rest)
	}
	return respError(fmt.Sprintf("unknown command '%s'", cmd))
}

func (f *fakeValkey) execSADD(args wireArgs) []byte {
	key := args[0]
	f.aliveLocked(key)
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

func (f *fakeValkey) execSCARD(args wireArgs) []byte {
	key := args[0]
	if !f.aliveLocked(key) {
		return respInt(0)
	}
	return respInt(int64(len(f.sets[key])))
}

type expireArgs struct {
	Key     string
	Seconds int
	Mode    string
}

func parseExpireArgs(args wireArgs) expireArgs {
	e := expireArgs{Key: args[0]}
	e.Seconds, _ = strconv.Atoi(args[1])
	if len(args) > 2 {
		e.Mode = strings.ToUpper(args[2])
	}
	return e
}

func (f *fakeValkey) execEXPIRE(args wireArgs) []byte {
	e := parseExpireArgs(args)
	target := f.nowFunc().Add(time.Duration(e.Seconds) * time.Second)
	f.aliveLocked(e.Key)
	current, hasCurrent := f.expires[e.Key]
	switch e.Mode {
	case "NX":
		if hasCurrent {
			return respInt(0)
		}
	case "GT":
		if !hasCurrent || !target.After(current) {
			return respInt(0)
		}
	}
	f.expires[e.Key] = target
	return respInt(1)
}

type hashPair struct {
	Field string
	Value string
}

func parseHashPairs(args wireArgs) ([]hashPair, error) {
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("wrong number of arguments for HSET")
	}
	pairs := make([]hashPair, 0, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		pairs = append(pairs, hashPair{Field: args[i], Value: args[i+1]})
	}
	return pairs, nil
}

func (f *fakeValkey) execHSET(args wireArgs) []byte {
	key := args[0]
	pairs, err := parseHashPairs(args[1:])
	if err != nil {
		return respError(err.Error())
	}
	f.aliveLocked(key)
	if f.hashes[key] == nil {
		f.hashes[key] = map[string]string{}
	}
	added := int64(0)
	for _, p := range pairs {
		if _, exists := f.hashes[key][p.Field]; !exists {
			added++
		}
		f.hashes[key][p.Field] = p.Value
	}
	return respInt(added)
}

func (f *fakeValkey) execHGET(args wireArgs) []byte {
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

func (f *fakeValkey) execDEL(args wireArgs) []byte {
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

func (f *fakeValkey) execEXISTS(args wireArgs) []byte {
	n := int64(0)
	for _, key := range args {
		if f.aliveLocked(key) {
			n++
		}
	}
	return respInt(n)
}

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
