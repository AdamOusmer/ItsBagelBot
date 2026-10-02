// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package botstatus

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	valkeygo "github.com/valkey-io/valkey-go"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestReporter() (*Reporter, *clock) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	r := New(nil, "pod-1", nil)
	r.now = c.now
	return r, c
}

type respArgs []string

type valkeyFake struct {
	mu      sync.Mutex
	members map[string]float64
	strings map[string]string
}

func newZsetFake(t *testing.T) valkeygo.Client {
	t.Helper()
	_, client := newValkeyFake(t)
	return client
}

func newValkeyFake(t *testing.T) (*valkeyFake, valkeygo.Client) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake valkey listen: %v", err)
	}
	f := &valkeyFake{members: map[string]float64{}, strings: map[string]string{}}
	go f.serve(ln)
	client, err := valkeygo.NewClient(valkeygo.ClientOption{
		InitAddress:  []string{ln.Addr().String()},
		DisableCache: true,
	})
	if err != nil {
		t.Fatalf("fake valkey client: %v", err)
	}
	t.Cleanup(func() {
		client.Close()
		_ = ln.Close()
	})
	return f, client
}

func (f *valkeyFake) value(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.strings[key]
}

func (f *valkeyFake) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go f.session(conn)
	}
}

func (f *valkeyFake) session(c net.Conn) {
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

func (f *valkeyFake) exec(args respArgs) []byte {
	if len(args) == 0 {
		return respErr("empty command")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	handle, ok := f.commands()[strings.ToUpper(args[0])]
	if !ok {
		return respErr(fmt.Sprintf("unknown command '%s'", args[0]))
	}
	return handle(args[1:])
}

func (f *valkeyFake) commands() map[string]func(respArgs) []byte {
	ok := func(respArgs) []byte { return respSimple("OK") }
	zero := func(respArgs) []byte { return respInt(0) }
	return map[string]func(respArgs) []byte{
		"HELLO":            func(respArgs) []byte { return respErr("unknown command 'HELLO'") },
		"ZADD":             f.zadd,
		"ZRANGEBYSCORE":    f.zrangebyscore,
		"ZREMRANGEBYSCORE": zero,
		"EXPIRE":           zero,
		"SET":              f.set,
		"AUTH":             ok,
		"CLIENT":           ok,
		"SELECT":           ok,
		"COMMAND":          ok,
		"PING":             ok,
	}
}

func (f *valkeyFake) set(args respArgs) []byte {
	if len(args) < 2 {
		return respErr("set: want key value")
	}
	f.strings[args[0]] = args[1]
	return respSimple("OK")
}

func (f *valkeyFake) zadd(args respArgs) []byte {
	if len(args) != 3 {
		return respErr("zadd: want key score member")
	}
	score, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return respErr("zadd: " + err.Error())
	}
	f.members[args[2]] = score
	return respInt(1)
}

func (f *valkeyFake) zrangebyscore(args respArgs) []byte {
	if len(args) < 3 {
		return respErr("zrangebyscore: want key min max")
	}
	members := f.inRange(parseBound(args[1]), parseBound(args[2]))
	if len(args) > 3 && strings.EqualFold(args[3], "WITHSCORES") {
		return respArray(f.withScores(members))
	}
	return respArray(members)
}

func (f *valkeyFake) inRange(lo, hi float64) respArgs {
	members := respArgs{}
	for m, s := range f.members {
		if s >= lo && s <= hi {
			members = append(members, m)
		}
	}
	sort.Slice(members, func(i, j int) bool { return f.members[members[i]] < f.members[members[j]] })
	return members
}

func (f *valkeyFake) withScores(members respArgs) respArgs {
	out := make(respArgs, 0, 2*len(members))
	for _, m := range members {
		out = append(out, m, strconv.FormatFloat(f.members[m], 'f', -1, 64))
	}
	return out
}

func parseBound(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimPrefix(s, "("), 64)
	if err != nil {
		return 0
	}
	return v
}

func readRESPArray(r *bufio.Reader) (respArgs, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("want array, got %q", line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make(respArgs, 0, n)
	for range n {
		arg, err := readBulk(r)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func readBulk(r *bufio.Reader) (string, error) {
	line, err := readLine(r)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(line, "$") {
		return "", fmt.Errorf("want bulk string, got %q", line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return "", err
	}
	buf := make([]byte, n+2)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func respErr(msg string) []byte    { return []byte("-ERR " + msg + "\r\n") }
func respSimple(msg string) []byte { return []byte("+" + msg + "\r\n") }
func respInt(n int64) []byte       { return []byte(":" + strconv.FormatInt(n, 10) + "\r\n") }

func respArray(items respArgs) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(items))
	for _, it := range items {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(it), it)
	}
	return []byte(b.String())
}
