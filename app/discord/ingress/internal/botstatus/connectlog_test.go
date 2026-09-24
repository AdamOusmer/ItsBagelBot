// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package botstatus

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
	"testing"
	"time"

	valkeygo "github.com/valkey-io/valkey-go"
)

func TestConnectLogLoadReadsTheScoresBack(t *testing.T) {
	log := NewConnectLog(newZsetFake(t), "pod-1")
	ctx := context.Background()
	base := time.UnixMilli(1_700_000_000_000)
	for _, at := range []time.Time{base.Add(time.Second), base} {
		if err := log.Add(ctx, at); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	got, err := log.Load(ctx, base.Add(-time.Minute))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []time.Time{base, base.Add(time.Second)}
	if len(got) != len(want) {
		t.Fatalf("loaded %d attempts, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("attempt %d = %s, want %s (oldest first)", i, got[i], want[i])
		}
	}
}

type respArgs []string

type zsetFake struct {
	mu      sync.Mutex
	members map[string]float64
}

func newZsetFake(t *testing.T) valkeygo.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake valkey listen: %v", err)
	}
	f := &zsetFake{members: map[string]float64{}}
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
	return client
}

func (f *zsetFake) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go f.session(conn)
	}
}

func (f *zsetFake) session(c net.Conn) {
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

func (f *zsetFake) exec(args respArgs) []byte {
	if len(args) == 0 {
		return respErr("empty command")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch strings.ToUpper(args[0]) {
	case "HELLO":
		return respErr("unknown command 'HELLO'")
	case "ZADD":
		return f.zadd(args[1:])
	case "ZRANGEBYSCORE":
		return f.zrangebyscore(args[1:])
	case "ZREMRANGEBYSCORE", "EXPIRE":
		return respInt(0)
	case "AUTH", "CLIENT", "SELECT", "COMMAND", "PING":
		return respSimple("OK")
	}
	return respErr(fmt.Sprintf("unknown command '%s'", args[0]))
}

func (f *zsetFake) zadd(args respArgs) []byte {
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

func (f *zsetFake) zrangebyscore(args respArgs) []byte {
	if len(args) < 3 {
		return respErr("zrangebyscore: want key min max")
	}
	members := f.inRange(parseBound(args[1]), parseBound(args[2]))
	if len(args) > 3 && strings.EqualFold(args[3], "WITHSCORES") {
		return respArray(f.withScores(members))
	}
	return respArray(members)
}

func (f *zsetFake) inRange(lo, hi float64) respArgs {
	members := respArgs{}
	for m, s := range f.members {
		if s >= lo && s <= hi {
			members = append(members, m)
		}
	}
	sort.Slice(members, func(i, j int) bool { return f.members[members[i]] < f.members[members[j]] })
	return members
}

func (f *zsetFake) withScores(members respArgs) respArgs {
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
