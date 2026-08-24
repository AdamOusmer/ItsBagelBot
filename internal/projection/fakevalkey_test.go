// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

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

// This file is a stateful in-process fake Valkey: a minimal RESP2 server the
// real valkey-go client dials, so Store tests exercise genuine wire round
// trips (Do/DoMulti routing, pipelining) instead of mocked results.
//
// Why not a recording stub: tombstone skips and rev gating are decided from
// server STATE (EXISTS / stored revs), so the fake must remember hashes,
// string keys and expiries. Why not miniredis: not a dependency of this repo,
// and the command surface the projection uses is a dozen verbs.
//
// The server is deliberately single-threaded per connection — one connection,
// commands processed strictly in arrival order — which mirrors a real Valkey's
// serial execution and is exactly what makes op-block contiguity assertions
// meaningful for the shard-mutex tests.

type fakeOp struct {
	cmd  string
	args []string // key + rest, cmd excluded
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

	mu      sync.Mutex
	hashes  map[string]map[string]string
	strs    map[string]string
	expires map[string]time.Time
	log     []fakeOp
	done    chan struct{}
}

// newFakeValkey boots the listener + client. Caller must Close.
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
		DisableCache: true, // no CLIENT TRACKING init; the fake speaks plain RESP2
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

// ops snapshots the execution log.
func (f *fakeValkey) ops() []fakeOp {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeOp(nil), f.log...)
}

// hash returns a copy of one hash for assertions.
func (f *fakeValkey) hash(key string) fakeHash {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.hashes[key] {
		out[k] = v
	}
	return out
}

// seed writes a hash field bypassing the Store, to stage pre-existing state.
// fakeHash is one key's field map.
type fakeHash map[string]string

// fakeField is one hash field+value pair for seeding.
type fakeField struct {
	field string
	value string
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

// session serves one connection: read a command array, execute, write the
// reply, until either side drops.
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

// fakeCommandHandlers dispatches one supported command to its executor.
// Handlers run with f.mu HELD and receive the args AFTER the command word;
// they answer in RESP2 via the resp* helpers. Unknown commands fall to the
// default at the bottom of exec, matching the real server's error shape.
var fakeCommandHandlers = map[string]func(*fakeValkey, []string) []byte{
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

// exec runs one command atomically under the global fake lock and records it.
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
		// Match pipe.go's noHello regex so valkey-go falls back to RESP2.
		return respError("unknown command 'HELLO'")
	case "AUTH", "CLIENT", "SELECT", "COMMAND", "PING":
		return respSimple("OK")
	}
	if h, ok := fakeCommandHandlers[cmd]; ok {
		return h(f, rest)
	}
	return respError(fmt.Sprintf("unknown command '%s'", cmd))
}

func (f *fakeValkey) execHSET(args []string) []byte {
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

func (f *fakeValkey) execHGET(args []string) []byte {
	v, ok := f.hashes[args[0]][args[1]]
	if !ok || !f.aliveLocked(args[0]) {
		return respNil()
	}
	return respBulk(v)
}

func (f *fakeValkey) execHMGET(args []string) []byte {
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

func (f *fakeValkey) execHGETALL(args []string) []byte {
	h := f.hashes[args[0]]
	flat := make([]string, 0, len(h)*2)
	for k, v := range h {
		flat = append(flat, k, v)
	}
	return respFlatArray(flat)
}

func (f *fakeValkey) execHDEL(args []string) []byte {
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

func (f *fakeValkey) execDEL(args []string) []byte {
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

func (f *fakeValkey) execEXISTS(args []string) []byte {
	n := int64(0)
	for _, key := range args {
		if f.aliveLocked(key) {
			n = 1
		}
	}
	return respInt(n)
}

func (f *fakeValkey) execSET(args []string) []byte {
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

func (f *fakeValkey) execEXPIRE(args []string) []byte {
	key := args[0]
	secs, _ := strconv.Atoi(args[1])
	mode := ""
	if len(args) > 2 {
		mode = strings.ToUpper(args[2])
	}
	target := f.nowFunc().Add(time.Duration(secs) * time.Second)
	// Lazy-expire first so NX/GT see a truthful "has expiry" state.
	f.aliveLocked(key)
	current, hasCurrent := f.expires[key]
	switch mode {
	case "NX": // only when no expiry lives
		if hasCurrent {
			return respInt(0)
		}
	case "GT": // only extends an existing shorter expiry
		if !hasCurrent || !target.After(current) {
			return respInt(0)
		}
	}
	f.expires[key] = target
	return respInt(1)
}

// execEVAL implements just the scripts the projection layer issues:
// clearFieldsLua deletes every hash field prefixed by any ARGV entry.
func (f *fakeValkey) execEVAL(args []string) []byte {
	script := args[0]
	numkeys, _ := strconv.Atoi(args[1])
	keys := args[2 : 2+numkeys]
	argv := args[2+numkeys:]
	if !strings.Contains(script, "HKEYS") || !strings.Contains(script, "HDEL") {
		return respError("unsupported script in fake")
	}
	return respInt(f.deleteByPrefixes(keys[0], argv))
}

// deleteByPrefixes mirrors the projection sweep script: HDEL every field of
// key matching any prefix, returning the count.
func (f *fakeValkey) deleteByPrefixes(key string, prefixes []string) int64 {
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

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// aliveLocked applies lazy expiry; caller holds mu.
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

// --- RESP2 encoding helpers ---

// respCount reads one "*N"/"$N" header line and returns N.
func respCount(r *bufio.Reader, prefix byte) (int, error) {
	line, err := readLine(r)
	if err != nil {
		return 0, err
	}
	if len(line) == 0 || line[0] != prefix {
		return 0, fmt.Errorf("expected %q-prefixed header, got %q", prefix, line)
	}
	return strconv.Atoi(line[1:])
}

func readBulkString(r *bufio.Reader) (string, error) {
	size, err := respCount(r, '$')
	if err != nil {
		return "", err
	}
	buf := make([]byte, size+2) // payload + CRLF
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf[:size]), nil
}

func readRESPArray(r *bufio.Reader) ([]string, error) {
	n, err := respCount(r, '*')
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		arg, err := readBulkString(r)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func respSimple(s string) []byte { return []byte("+" + s + "\r\n") }
func respInt(v int64) []byte     { return []byte(":" + strconv.FormatInt(v, 10) + "\r\n") }
func respBulk(s string) []byte   { return []byte("$" + strconv.Itoa(len(s)) + "\r\n" + s + "\r\n") }
func respNil() []byte            { return []byte("$-1\r\n") }
func respError(s string) []byte  { return []byte("-ERR " + s + "\r\n") }

func respFlatArray(items []string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(items))
	for _, s := range items {
		b.Write(respBulk(s))
	}
	return []byte(b.String())
}
