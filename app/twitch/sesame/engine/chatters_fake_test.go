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

	valkey_go "github.com/valkey-io/valkey-go"
)

type chattersFake struct {
	ln     net.Listener
	client valkey_go.Client

	mu      sync.Mutex
	now     time.Time
	strs    map[string]string
	expires map[string]time.Time
	failGET bool
}

func newChattersFake(t *testing.T) *chattersFake {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake valkey listen: %v", err)
	}
	f := &chattersFake{
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

func (f *chattersFake) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func (f *chattersFake) breakGET() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failGET = true
}

func (f *chattersFake) rawValue(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.aliveLocked(key) {
		return "", false
	}
	return f.strs[key], true
}

func (f *chattersFake) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.session(conn)
	}
}

func (f *chattersFake) session(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		args, err := readChattersRESPArray(r)
		if err != nil {
			return
		}
		if _, err := c.Write(f.exec(args)); err != nil {
			return
		}
	}
}

type chattersRESPArgs []string

func (f *chattersFake) exec(args chattersRESPArgs) []byte {
	if len(args) == 0 {
		return chattersErr("empty command")
	}
	cmd, rest := strings.ToUpper(args[0]), args[1:]

	f.mu.Lock()
	defer f.mu.Unlock()
	switch cmd {
	case "HELLO":
		return chattersErr("unknown command 'HELLO'")
	case "AUTH", "CLIENT", "SELECT", "COMMAND", "PING":
		return chattersSimple("OK")
	case "SET":
		return f.execSET(rest)
	case "GET":
		return f.execGET(rest)
	case "DEL":
		return f.execDEL(rest)
	}
	return chattersErr(fmt.Sprintf("unknown command '%s'", cmd))
}

func (f *chattersFake) execSET(args chattersRESPArgs) []byte {
	key, val := args[0], args[1]
	nx, ttl := chattersSetOptions(args[2:])
	if nx && f.aliveLocked(key) {
		return chattersNil()
	}
	f.strs[key] = val
	delete(f.expires, key)
	if ttl > 0 {
		f.expires[key] = f.now.Add(ttl)
	}
	return chattersSimple("OK")
}

func (f *chattersFake) execGET(args chattersRESPArgs) []byte {
	if f.failGET {
		return chattersErr("SIMULATED transient read failure")
	}
	if !f.aliveLocked(args[0]) {
		return chattersNil()
	}
	return chattersBulk(f.strs[args[0]])
}

func (f *chattersFake) execDEL(args chattersRESPArgs) []byte {
	deleted := int64(0)
	for _, key := range args {
		if _, ok := f.strs[key]; ok {
			deleted++
		}
		delete(f.strs, key)
		delete(f.expires, key)
	}
	return chattersInt(deleted)
}

func chattersSetOptions(args chattersRESPArgs) (bool, time.Duration) {
	nx, ttl := false, time.Duration(0)
	for i := 0; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "NX":
			nx = true
		case "EX":
			ttl, i = time.Duration(chattersAtoi(args[i+1]))*time.Second, i+1
		case "PX":
			ttl, i = time.Duration(chattersAtoi(args[i+1]))*time.Millisecond, i+1
		}
	}
	return nx, ttl
}

func (f *chattersFake) aliveLocked(key string) bool {
	if deadline, ok := f.expires[key]; ok && !f.now.Before(deadline) {
		delete(f.strs, key)
		delete(f.expires, key)
		return false
	}
	_, ok := f.strs[key]
	return ok
}

func chattersAtoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func chattersSimple(s string) []byte { return []byte("+" + s + "\r\n") }
func chattersInt(v int64) []byte     { return []byte(":" + strconv.FormatInt(v, 10) + "\r\n") }
func chattersBulk(s string) []byte   { return []byte("$" + strconv.Itoa(len(s)) + "\r\n" + s + "\r\n") }
func chattersNil() []byte            { return []byte("$-1\r\n") }
func chattersErr(s string) []byte    { return []byte("-ERR " + s + "\r\n") }

func readChattersRESPArray(r *bufio.Reader) (chattersRESPArgs, error) {
	n, err := readChattersCount(r, '*')
	if err != nil {
		return nil, err
	}
	args := make(chattersRESPArgs, 0, n)
	for i := 0; i < n; i++ {
		arg, err := readChattersBulk(r)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func readChattersBulk(r *bufio.Reader) (string, error) {
	size, err := readChattersCount(r, '$')
	if err != nil {
		return "", err
	}
	buf := make([]byte, size+2)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf[:size]), nil
}

func readChattersCount(r *bufio.Reader, prefix byte) (int, error) {
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
