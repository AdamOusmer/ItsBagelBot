// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkeytest

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"
)

type Args []string

func (a Args) Key() string { return a[0] }

type Reply []byte

type Handler func(s *Server, a Args) Reply

type Op struct {
	Cmd  string
	Args Args
}

type Failure struct {
	Cmd     string
	Message string
	Match   func(a Args) bool
}

// Handlers and With callbacks run under the server lock: they may use the exported maps and Now, never the other methods.
type Server struct {
	ln     net.Listener
	client valkey.Client

	mu       sync.Mutex
	offset   time.Duration
	log      []Op
	handlers map[string]Handler
	failures map[string]Failure

	Strings map[string]string
	Hashes  map[string]map[string]string
	Sets    map[string]map[string]bool
	Lists   map[string][]string
	Expires map[string]time.Time
}

func New(t testing.TB) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("valkeytest listen: %v", err)
	}
	s := &Server{
		ln:       ln,
		handlers: defaultHandlers(),
		failures: map[string]Failure{},
		Strings:  map[string]string{},
		Hashes:   map[string]map[string]string{},
		Sets:     map[string]map[string]bool{},
		Lists:    map[string][]string{},
		Expires:  map[string]time.Time{},
	}
	go s.serve()
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{ln.Addr().String()}, DisableCache: true})
	if err != nil {
		t.Fatalf("valkeytest client: %v", err)
	}
	s.client = client
	t.Cleanup(func() {
		client.Close()
		_ = ln.Close()
	})
	return s
}

func (s *Server) Client() valkey.Client { return s.client }

func (s *Server) Handle(cmd string, h Handler) {
	s.With(func(s *Server) { s.handlers[strings.ToUpper(cmd)] = h })
}

func (s *Server) Fail(f Failure) {
	s.With(func(s *Server) { s.failures[strings.ToUpper(f.Cmd)] = f })
}

func (s *Server) Heal(cmd string) {
	s.With(func(s *Server) { delete(s.failures, strings.ToUpper(cmd)) })
}

func (s *Server) Advance(d time.Duration) {
	s.With(func(s *Server) { s.offset += d })
}

func (s *Server) With(fn func(s *Server)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s)
}

func (s *Server) Now() time.Time { return time.Now().Add(s.offset) }

func (s *Server) Ops() []Op {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Op(nil), s.log...)
}

func (s *Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.session(conn)
	}
}

func (s *Server) session(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		args, err := readArray(r)
		if err != nil {
			return
		}
		if _, err := c.Write(s.exec(args)); err != nil {
			return
		}
	}
}

func (s *Server) exec(args Args) Reply {
	if len(args) == 0 {
		return Err("empty command")
	}
	cmd, rest := strings.ToUpper(args[0]), args[1:]
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, Op{Cmd: cmd, Args: rest})
	switch cmd {
	case "HELLO":
		return Err("unknown command 'HELLO'")
	case "AUTH", "CLIENT", "SELECT", "COMMAND", "PING":
		return Simple("OK")
	}
	if f, ok := s.failures[cmd]; ok && f.matches(rest) {
		return Err(f.Message)
	}
	if h, ok := s.handlers[cmd]; ok {
		return h(s, rest)
	}
	return Err(fmt.Sprintf("unknown command '%s'", cmd))
}

func (f Failure) matches(a Args) bool { return f.Match == nil || f.Match(a) }

func (s *Server) Alive(key string) bool {
	if deadline, ok := s.Expires[key]; ok && !s.Now().Before(deadline) {
		s.drop(key)
		return false
	}
	_, str := s.Strings[key]
	_, hash := s.Hashes[key]
	_, set := s.Sets[key]
	_, list := s.Lists[key]
	return str || hash || set || list
}

func (s *Server) drop(key string) {
	delete(s.Strings, key)
	delete(s.Hashes, key)
	delete(s.Sets, key)
	delete(s.Lists, key)
	delete(s.Expires, key)
}
