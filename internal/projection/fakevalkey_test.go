// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

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

type fakeOp struct {
	cmd  string
	args []string
}

func (o fakeOp) key() string {
	if len(o.args) > 0 {
		return o.args[0]
	}
	return ""
}

type fakeValkey struct {
	t       *testing.T
	ln      net.Listener
	addr    string
	client  valkey_go.Client
	nowFunc func() time.Time

	mu       sync.Mutex
	hashes   map[string]fakeHash
	strs     map[string]string
	expires  map[string]time.Time
	log      []fakeOp
	done     chan struct{}
	hgetFail string
}

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
		hashes:  map[string]fakeHash{},
		strs:    map[string]string{},
		expires: map[string]time.Time{},
		done:    make(chan struct{}),
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
	t.Cleanup(f.Close)
	return f
}

func (f *fakeValkey) Close() {
	if f.client != nil {
		f.client.Close()
	}
	_ = f.ln.Close()
}

func (f *fakeValkey) ops() []fakeOp {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeOp(nil), f.log...)
}

func (f *fakeValkey) hash(key string) fakeHash {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.hashes[key] {
		out[k] = v
	}
	return out
}

type cmdArgs []string

type fakeHash map[string]string

type fakeField struct {
	field string
	value string
}

func (f *fakeValkey) failHGET(field string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hgetFail = field
}

func (f *fakeValkey) seed(key string, kv fakeField) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hashes[key] == nil {
		f.hashes[key] = fakeHash{}
	}
	f.hashes[key][kv.field] = kv.value
}

func (f *fakeValkey) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			close(f.done)
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

var fakeCommandHandlers = map[string]func(*fakeValkey, cmdArgs) []byte{
	"HSET":    (*fakeValkey).execHSET,
	"HGET":    (*fakeValkey).execHGET,
	"HMGET":   (*fakeValkey).execHMGET,
	"HGETALL": (*fakeValkey).execHGETALL,
	"HDEL":    (*fakeValkey).execHDEL,
	"DEL":     (*fakeValkey).execDEL,
	"EXISTS":  (*fakeValkey).execEXISTS,
	"SET":     (*fakeValkey).execSET,
	"EXPIRE":  (*fakeValkey).execEXPIRE,
	"EVAL":    (*fakeValkey).execEVAL,
}

func (f *fakeValkey) exec(args cmdArgs) []byte {
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

func (f *fakeValkey) execHSET(args cmdArgs) []byte {
	key := args[0]
	pairs := args[1:]
	if len(pairs)%2 != 0 {
		return respError("wrong number of arguments for HSET")
	}
	if f.hashes[key] == nil {
		f.hashes[key] = fakeHash{}
	}
	added := 0
	for i := 0; i < len(pairs); i += 2 {
		if _, exists := f.hashes[key][pairs[i]]; !exists {
			added++
		}
		f.hashes[key][pairs[i]] = pairs[i+1]
	}
	return respInt(int64(added))
}

func (f *fakeValkey) execHGET(args cmdArgs) []byte {
	if args[1] == f.hgetFail {
		return respError("SIMULATED transient read failure")
	}
	v, ok := f.hashes[args[0]][args[1]]
	if !ok || !f.aliveLocked(args[0]) {
		return respNil()
	}
	return respBulk(v)
}

func (f *fakeValkey) execHMGET(args cmdArgs) []byte {
	h := f.hashes[args[0]]
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args)-1)
	for _, field := range args[1:] {
		if v, ok := h[field]; ok && f.aliveLocked(args[0]) {
			b.Write(respBulk(v))
		} else {
			b.Write(respNil())
		}
	}
	return []byte(b.String())
}

func (f *fakeValkey) execHGETALL(args cmdArgs) []byte {
	h := f.hashes[args[0]]
	flat := make([]string, 0, len(h)*2)
	for k, v := range h {
		flat = append(flat, k, v)
	}
	return respFlatArray(flat)
}

func (f *fakeValkey) execHDEL(args cmdArgs) []byte {
	key := args[0]
	h, ok := f.hashes[key]
	if !ok || !f.aliveLocked(key) {
		return respInt(0)
	}
	removed := int64(0)
	for _, field := range args[1:] {
		if _, exists := h[field]; exists {
			delete(h, field)
			removed++
		}
	}
	return respInt(removed)
}

func (f *fakeValkey) execDEL(args cmdArgs) []byte {
	deleted := int64(0)
	for _, key := range args {
		if _, ok := f.hashes[key]; ok {
			delete(f.hashes, key)
			deleted = 1
		}
		if _, ok := f.strs[key]; ok {
			delete(f.strs, key)
			deleted = 1
		}
		delete(f.expires, key)
	}
	return respInt(deleted)
}

func (f *fakeValkey) execEXISTS(args cmdArgs) []byte {
	n := int64(0)
	for _, key := range args {
		if f.aliveLocked(key) {
			n = 1
		}
	}
	return respInt(n)
}

func (f *fakeValkey) execSET(args cmdArgs) []byte {
	key := args[0]
	f.strs[key] = args[1]
	for i := 2; i+1 < len(args); i += 2 {
		if strings.EqualFold(args[i], "EX") {
			secs, _ := strconv.Atoi(args[i+1])
			f.expires[key] = f.nowFunc().Add(time.Duration(secs) * time.Second)
		}
	}
	return respSimple("OK")
}

func (f *fakeValkey) execEXPIRE(args cmdArgs) []byte {
	key := args[0]
	secs, _ := strconv.Atoi(args[1])
	mode := ""
	if len(args) > 2 {
		mode = strings.ToUpper(args[2])
	}
	target := f.nowFunc().Add(time.Duration(secs) * time.Second)
	f.aliveLocked(key)
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

func (f *fakeValkey) execEVAL(args cmdArgs) []byte {
	script := args[0]
	numkeys, _ := strconv.Atoi(args[1])
	keys := args[2 : 2+numkeys]
	argv := args[2+numkeys:]
	if !strings.Contains(script, "HKEYS") || !strings.Contains(script, "HDEL") {
		return respError("unsupported script in fake")
	}
	return respInt(f.deleteByPrefixes(keys[0], argv))
}

func (f *fakeValkey) deleteByPrefixes(key string, prefixes cmdArgs) int64 {
	h, ok := f.hashes[key]
	if !ok || !f.aliveLocked(key) {
		return 0
	}
	removed := int64(0)
	for field := range h {
		if hasAnyPrefix(field, prefixes) {
			delete(h, field)
			removed++
		}
	}
	return removed
}

func hasAnyPrefix(s string, prefixes cmdArgs) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func (f *fakeValkey) aliveLocked(key string) bool {
	if dl, ok := f.expires[key]; ok {
		if f.nowFunc().After(dl) {
			delete(f.hashes, key)
			delete(f.strs, key)
			delete(f.expires, key)
			return false
		}
	}
	_, hash := f.hashes[key]
	_, str := f.strs[key]
	return hash || str
}
