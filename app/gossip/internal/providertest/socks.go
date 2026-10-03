// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package providertest

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"

	"github.com/stretchr/testify/require"
)

var socksRefused = []byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}

type FakeSOCKS struct {
	ln net.Listener
	wg sync.WaitGroup

	mu       sync.Mutex
	refusing bool
	conns    atomic.Int32
}

func NewFakeSOCKS(t testing.TB) *FakeSOCKS {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &FakeSOCKS{ln: ln}
	prev := core.WARPProxyAddr()
	core.SetWARPProxyAddrForTests(ln.Addr().String())
	t.Cleanup(func() {
		core.SetWARPProxyAddrForTests(prev)
		_ = ln.Close()
		f.wg.Wait()
	})
	f.wg.Add(1)
	go f.serve()
	return f
}

func (f *FakeSOCKS) SetRefusing(v bool) {
	f.mu.Lock()
	f.refusing = v
	f.mu.Unlock()
}

func (f *FakeSOCKS) Conns() int { return int(f.conns.Load()) }

func (f *FakeSOCKS) serve() {
	defer f.wg.Done()
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			defer conn.Close()
			f.handle(conn)
		}()
	}
}

func (f *FakeSOCKS) handle(conn net.Conn) {
	if !socksGreet(conn) {
		return
	}
	target, ok := readSOCKSTarget(conn)
	if !ok {
		return
	}
	f.pipe(conn, target)
}

func socksGreet(conn net.Conn) bool {
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil || head[0] != 5 {
		return false
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return false
	}
	_, err := conn.Write([]byte{5, 0})
	return err == nil
}

func readSOCKSConnect(conn net.Conn) (atyp byte, ok bool) {
	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil {
		return 0, false
	}
	return req[3], req[0] == 5 && req[1] == 1
}

func readSOCKSTarget(conn net.Conn) (string, bool) {
	atyp, ok := readSOCKSConnect(conn)
	if !ok {
		return "", false
	}
	host, ok := readSOCKSHost(conn, atyp)
	if !ok {
		return "", false
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port)))), true
}

func readSOCKSHost(conn net.Conn, atyp byte) (string, bool) {
	switch atyp {
	case 1, 4:
		ip := make([]byte, map[byte]int{1: 4, 4: 16}[atyp])
		_, err := io.ReadFull(conn, ip)
		return net.IP(ip).String(), err == nil
	case 3:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return "", false
		}
		name := make([]byte, length[0])
		_, err := io.ReadFull(conn, name)
		return string(name), err == nil
	}
	return "", false
}

func (f *FakeSOCKS) pipe(conn net.Conn, target string) {
	f.mu.Lock()
	refuse := f.refusing
	f.mu.Unlock()
	upstream, err := net.Dial("tcp", target)
	if err != nil || refuse {
		if err == nil {
			_ = upstream.Close()
		}
		_, _ = conn.Write(socksRefused)
		return
	}
	f.conns.Add(1)
	_, _ = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	go func() { _, _ = io.Copy(upstream, conn); _ = upstream.(*net.TCPConn).CloseWrite() }()
	_, _ = io.Copy(conn, upstream)
	_ = upstream.Close()
}
