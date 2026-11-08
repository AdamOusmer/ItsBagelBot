// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build linux && (amd64 || arm64)

package sandbox

import (
	"bufio"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var linuxHelper = flag.Bool("sandbox-platform-helper", false, "run isolated test helper")
var linuxSecret = flag.String("sandbox-parent-secret", "", "private parent fixture path")
var linuxDescriptors = flag.String("sandbox-fds", "", "inherited stream descriptors")

func TestLinuxRejectsAmbientFileDescriptors(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "private-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if linuxPipes([]*os.File{file}) {
		t.Fatal("regular files must not cross the sandbox descriptor boundary")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if !linuxPipes([]*os.File{reader, writer}) {
		t.Fatal("explicit stream pipes were rejected")
	}
}

func TestLinuxPolicyCannotBeRewritten(t *testing.T) {
	file, err := linuxPolicy()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.Write([]byte{0}); err != unix.EPERM && !os.IsPermission(err) {
		t.Fatal("seccomp policy must be immutable after creation", err)
	}
}

func TestLinuxKernelSandbox(t *testing.T) {
	if os.Getenv("BAGEL_SANDBOX_KERNEL_TEST") != "1" {
		t.Skip("opt-in native kernel isolation; build/run with CGO_ENABLED=0 and bubblewrap installed")
	}
	executable, err := os.Executable()
	if err != nil || !staticLinuxWorker(executable) {
		t.Fatal("native kernel test requires a static CGO_ENABLED=0 test binary")
	}
	secret := linuxPrivateFixture(t)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	process, err := Start(ctx, Config{Executable: executable, Args: []string{"-test.run=^TestLinuxIsolatedHelper$", "--sandbox-platform-helper", "--sandbox-parent-secret=" + secret}, ExtraFiles: []*os.File{reader}})
	if err != nil {
		t.Fatal("required OS sandbox could not start", err)
	}
	defer process.Close()
	_ = reader.Close()
	if _, err = writer.Write([]byte("broker-bytes")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(process.Output).ReadString('\n')
	if err != nil || line != "isolated\n" {
		t.Fatal("child failed kernel/descriptor/filesystem isolation", err)
	}
	if err = process.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func linuxPrivateFixture(t *testing.T) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "parent-private")
	if err := os.WriteFile(name, []byte("must-not-cross-the-boundary"), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestLinuxIsolatedHelper(t *testing.T) {
	if !*linuxHelper {
		t.Skip("isolated helper only")
	}
	if !ChildIsolated() {
		os.Exit(11)
	}
	linuxCheckFinalFilter(t)
	if _, err := os.ReadFile(*linuxSecret); err == nil {
		os.Exit(12)
	}
	fd, err := strconv.Atoi(strings.Split(*linuxDescriptors, ",")[0])
	if err != nil {
		os.Exit(13)
	}
	stream := os.NewFile(uintptr(fd), "broker-stream")
	var payload [12]byte
	if _, err = io.ReadFull(stream, payload[:]); err != nil || string(payload[:]) != "broker-bytes" {
		os.Exit(14)
	}
	_, _ = os.Stdout.WriteString("isolated\n")
}

func linuxCheckFinalFilter(t *testing.T) {
	t.Helper()
	if err := unix.Exec("/nonexistent", nil, nil); err != unix.EPERM {
		t.Fatalf("exec not denied: %v", err)
	}
	_, _, err := unix.RawSyscall(unix.SYS_CLONE3, 0, 0, 0)
	if err != unix.ENOSYS {
		t.Fatalf("clone3 not denied: %v", err)
	}
	// Force additional OS threads after installing TSYNC, exercising Go's clone
	// fallback rather than only scheduling goroutines on pre-existing threads.
	var started, finished sync.WaitGroup
	release := make(chan struct{})
	started.Add(8)
	finished.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer finished.Done()
			started.Done()
			<-release
		}()
	}
	started.Wait()
	close(release)
	finished.Wait()
}

func TestLinuxFinalFilterRestrictsProcesses(t *testing.T) {
	filter := linuxProcessFilter()
	cases := []struct {
		number uint32
		flags  uint32
		want   uint32
	}{
		{unix.SYS_EXECVE, 0, unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{unix.SYS_EXECVEAT, 0, unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{unix.SYS_CLONE3, 0, unix.SECCOMP_RET_ERRNO | uint32(unix.ENOSYS)},
		{unix.SYS_CLONE, 0, unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{unix.SYS_CLONE, unix.CLONE_VM, unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{unix.SYS_CLONE, unix.CLONE_VM | unix.CLONE_THREAD, unix.SECCOMP_RET_ALLOW},
		{unix.SYS_READ, 0, unix.SECCOMP_RET_ALLOW},
		{unix.SYS_SOCKET, 0, unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
	}
	for _, tc := range cases {
		if got := linuxFilterResult(t, filter, tc.number, tc.flags); got != tc.want {
			t.Errorf("syscall %d flags %x: %x, want %x", tc.number, tc.flags, got, tc.want)
		}
	}
}
func linuxFilterResult(t *testing.T, filter []unix.SockFilter, number, flags uint32) uint32 {
	t.Helper()
	arch := uint32(unix.AUDIT_ARCH_X86_64)
	if runtime.GOARCH == "arm64" {
		arch = unix.AUDIT_ARCH_AARCH64
	}
	data := map[uint32]uint32{0: number, 4: arch, 16: flags}
	var accumulator uint32
	for i := 0; i < len(filter); i++ {
		instruction := filter[i]
		switch instruction.Code {
		case unix.BPF_LD | unix.BPF_W | unix.BPF_ABS:
			accumulator = data[instruction.K]
		case unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K:
			i += linuxFilterJump(accumulator == instruction.K, instruction)
		case unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K:
			i += linuxFilterJump(accumulator&instruction.K != 0, instruction)
		case unix.BPF_RET | unix.BPF_K:
			return instruction.K
		default:
			t.Fatalf("unknown filter instruction %x", instruction.Code)
		}
	}
	t.Fatal("filter did not return")
	return 0
}
func linuxFilterJump(match bool, instruction unix.SockFilter) int {
	if match {
		return int(instruction.Jt)
	}
	return int(instruction.Jf)
}
