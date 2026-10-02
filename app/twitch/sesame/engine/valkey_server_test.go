// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"
)

var localValkeySrv struct {
	once sync.Once
	addr string
	dir  string
	cmd  *exec.Cmd
	err  error
}

func TestMain(m *testing.M) {
	code := m.Run()
	stopLocalValkey()
	os.Exit(code)
}

func localValkeyAddr(tb testingTB) string {
	tb.Helper()
	localValkeySrv.once.Do(startLocalValkey)
	if localValkeySrv.err != nil {
		tb.Skip("VALKEY_TEST_ADDR is not set and no local valkey-server: " + localValkeySrv.err.Error())
	}
	return localValkeySrv.addr
}

func startLocalValkey() {
	binary, err := exec.LookPath("valkey-server")
	if err != nil {
		localValkeySrv.err = err
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		localValkeySrv.err = err
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	if localValkeySrv.dir, err = os.MkdirTemp("", "engine-valkey-"); err != nil {
		localValkeySrv.err = err
		return
	}
	cmd := exec.Command(binary, "--port", strconv.Itoa(port), "--bind", "127.0.0.1", "--save", "", "--appendonly", "no", "--notify-keyspace-events", "Ex", "--dir", localValkeySrv.dir)
	if err = cmd.Start(); err != nil {
		localValkeySrv.err = err
		return
	}
	localValkeySrv.cmd = cmd
	localValkeySrv.addr = "127.0.0.1:" + strconv.Itoa(port)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if conn, dialErr := net.Dial("tcp", localValkeySrv.addr); dialErr == nil {
			_ = conn.Close()
			return
		}
	}
	localValkeySrv.err = os.ErrDeadlineExceeded
}

func stopLocalValkey() {
	if localValkeySrv.cmd != nil {
		_ = localValkeySrv.cmd.Process.Kill()
		_, _ = localValkeySrv.cmd.Process.Wait()
	}
	if localValkeySrv.dir != "" {
		_ = os.RemoveAll(localValkeySrv.dir)
	}
}
