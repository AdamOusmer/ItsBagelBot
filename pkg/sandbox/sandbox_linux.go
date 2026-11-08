// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"debug/elf"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Bubblewrap is an OS prerequisite, not an optional optimization. Namespaces,
// read-only mounts, no-new-privileges and seccomp must all be available.
// https://github.com/containers/bubblewrap/blob/main/bubblewrap.c
func platformStart(ctx context.Context, config Config) (*Process, error) {
	if !staticLinuxWorker(config.Executable) || !linuxPipes(config.ExtraFiles) {
		return nil, ErrConfiguration
	}
	policy, err := linuxPolicy()
	if err != nil {
		return nil, ErrUnavailable
	}
	defer policy.Close()
	args, err := linuxArguments(config, len(config.ExtraFiles)+3)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("/usr/bin/bwrap", args...)
	cmd.Env = []string{}
	cmd.ExtraFiles = append(append([]*os.File(nil), config.ExtraFiles...), policy)
	return startLinuxCommand(ctx, cmd)
}

func linuxPipes(files []*os.File) bool {
	for _, file := range files {
		info, err := file.Stat()
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			return false
		}
	}
	return true
}

func startLinuxCommand(ctx context.Context, cmd *exec.Cmd) (*Process, error) {
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrUnavailable
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, ErrUnavailable
	}
	if err = cmd.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, ErrUnavailable
	}
	return newProcess(ctx, input, output, cmd.Wait, cmd.Process.Kill, nil), nil
}

func staticLinuxWorker(name string) bool {
	file, err := elf.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	for _, program := range file.Progs {
		if program.Type == elf.PT_INTERP {
			return false
		}
	}
	return file.Type == elf.ET_EXEC || file.Type == elf.ET_DYN
}

func linuxArguments(config Config, policyFD int) ([]string, error) {
	ca, err := linuxCABundle()
	if err != nil {
		return nil, err
	}
	args := []string{
		"--unshare-all", "--unshare-user", "--unshare-pid", "--unshare-net",
		"--die-with-parent", "--new-session", "--cap-drop", "ALL",
		"--clearenv", "--setenv", "GOMAXPROCS", "2", "--setenv", "GOMEMLIMIT", "128MiB",
		"--setenv", "SSL_CERT_FILE", "/etc/ssl/certs/ca-certificates.crt",
		"--setenv", "SSL_CERT_DIR", "/nonexistent", "--chdir", "/",
		"--ro-bind", config.Executable, "/worker", "--proc", "/proc", "--dev", "/dev",
		"--dir", "/etc", "--dir", "/etc/ssl", "--dir", "/etc/ssl/certs",
		"--ro-bind", ca, "/etc/ssl/certs/ca-certificates.crt", "--remount-ro", "/",
		"--seccomp", strconv.Itoa(policyFD), "--", "/worker",
	}
	args = append(args, config.Args...)
	return append(args, descriptorArgument(config.ExtraFiles, false)), nil
}

func linuxCABundle() (string, error) {
	for _, name := range []string{"/etc/ssl/certs/ca-certificates.crt", "/etc/pki/tls/certs/ca-bundle.crt", "/etc/ssl/ca-bundle.pem"} {
		target, err := filepath.EvalSymlinks(name)
		if err != nil {
			continue
		}
		info, err := os.Stat(target)
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= 4<<20 {
			return target, nil
		}
	}
	return "", ErrUnavailable
}

// The filter is inherited by all worker threads and descendants. Network
// namespaces also deny host routing; filtering socket creation independently
// prevents loopback/Unix socket access and inherited-descriptor network escape.
func linuxFilter() []unix.SockFilter {
	architecture := uint32(unix.AUDIT_ARCH_X86_64)
	if runtime.GOARCH == "arm64" {
		architecture = unix.AUDIT_ARCH_AARCH64
	}
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: 1, K: architecture},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		// x32 shares AUDIT_ARCH_X86_64 but uses a different syscall table.
		{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, Jf: 1, K: 0x40000000},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
	}
	for _, syscall := range []uint32{unix.SYS_SOCKET, unix.SYS_SOCKETPAIR, unix.SYS_CONNECT, unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV, unix.SYS_PIDFD_GETFD, unix.SYS_BPF, unix.SYS_IO_URING_SETUP, unix.SYS_UNSHARE, unix.SYS_SETNS, unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT, unix.SYS_MOVE_MOUNT, unix.SYS_OPEN_TREE} {
		filter = append(filter, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: syscall}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)})
	}
	return append(filter, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
}

func linuxPolicy() (*os.File, error) {
	fd, err := unix.MemfdCreate("bagel-worker-policy", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "sandbox-policy")
	if err = writeLinuxPolicy(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func writeLinuxPolicy(file *os.File) error {
	if err := binary.Write(file, binary.LittleEndian, linuxFilter()); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	_, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_SEAL)
	return err
}

// ChildIsolated is called before worker IPC reads or secret bootstrap. A child
// must observe kernel-enforced seccomp, no capabilities, and a minimal root.
func ChildIsolated() bool {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil || !linuxStatus(string(status)) || !linuxRoot() {
		return false
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err == nil {
		_ = unix.Close(fd)
		return false
	}
	return err == unix.EPERM && linuxLockProcess()
}

func linuxStatus(status string) bool {
	return strings.Contains(status, "NoNewPrivs:\t1\n") && strings.Contains(status, "Seccomp:\t2\n") && strings.Contains(status, "CapEff:\t0000000000000000\n")
}

func linuxRoot() bool {
	entries, err := os.ReadDir("/")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "worker", "proc", "dev", "etc":
		default:
			return false
		}
	}
	return true
}

// Layer the final restriction after bwrap has executed /worker. TSYNC applies
// it to every existing Go runtime thread; newly cloned threads inherit it.
// https://www.kernel.org/doc/html/latest/userspace-api/seccomp_filter.html
func linuxLockProcess() bool {
	filter := linuxProcessFilter()
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	result, _, errno := unix.RawSyscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	runtime.KeepAlive(filter)
	return result == 0 && errno == 0
}

func linuxProcessFilter() []unix.SockFilter {
	filter := linuxFilter()
	filter = filter[:len(filter)-1]
	denied := []uint32{unix.SYS_EXECVE, unix.SYS_EXECVEAT}
	if runtime.GOARCH == "amd64" {
		// fork/vfork have no separate syscall on arm64.
		denied = append(denied, 57, 58)
	}
	for _, number := range denied {
		filter = append(filter, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: number}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)})
	}
	return append(filter,
		// clone3 points to a struct that BPF cannot inspect. ENOSYS allows runtime
		// implementations to fall back to clone with flags visible to this filter.
		unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: unix.SYS_CLONE3},
		unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.ENOSYS)},
		unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 3, K: unix.SYS_CLONE},
		unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
		unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, Jt: 1, K: unix.CLONE_THREAD},
		unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	)
}
