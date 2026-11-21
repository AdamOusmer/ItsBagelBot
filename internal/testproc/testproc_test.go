// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package testproc_test

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"ItsBagelBot/internal/testproc"
)

const childEnv = "TESTPROC_CHILD_PIDFILE"

func TestMain(m *testing.M) {
	if pidfile := os.Getenv(childEnv); pidfile != "" {
		_, _ = testproc.Start("/bin/sh", "-c", "echo $$ > "+pidfile+"; exec sleep 600")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func serverPID(pidfile string) int {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		raw, _ := os.ReadFile(pidfile)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
			return pid
		}
	}
	return 0
}

func TestServerDiesWithKilledParent(t *testing.T) {
	pidfile := t.TempDir() + "/server.pid"
	parent := exec.Command(os.Args[0], "-test.run", "^$")
	parent.Env = append(os.Environ(), childEnv+"="+pidfile)
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	server := serverPID(pidfile)
	if server == 0 {
		t.Fatal("helper server never started")
	}
	_ = parent.Process.Kill()
	_ = parent.Wait()
	for deadline := time.Now().Add(5 * time.Second); alive(server) && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(server) {
		_ = syscall.Kill(server, syscall.SIGKILL)
		t.Fatalf("server %d outlived its killed parent", server)
	}
}

func TestStopEndsServer(t *testing.T) {
	pidfile := t.TempDir() + "/server.pid"
	p, err := testproc.Start("/bin/sh", "-c", "echo $$ > "+pidfile+"; exec sleep 600")
	if err != nil {
		t.Fatal(err)
	}
	server := serverPID(pidfile)
	p.Stop()
	if server == 0 || alive(server) {
		t.Fatalf("server %d still running after Stop", server)
	}
}
