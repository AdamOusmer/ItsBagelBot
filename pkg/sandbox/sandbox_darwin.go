// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build darwin

package sandbox

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

const darwinWorkerIdentifier = "com.itsbagelbot.desktop.network-worker"
const darwinCanaryEnvironment = "BAGEL_SANDBOX_CANARY"
const darwinDenyFork = "(deny process-fork)"
const darwinDenyExec = "(deny process-exec)"
const darwinDenyKeychain = `(deny mach-lookup (global-name-prefix "com.apple.securityd") (global-name-prefix "com.apple.SecurityServer"))`

func platformStart(ctx context.Context, config Config) (*Process, error) {
	if !darwinSignedWorker(ctx, config.Executable) {
		return nil, ErrUnavailable
	}
	canary, err := darwinCanary()
	if err != nil {
		return nil, ErrUnavailable
	}
	cleanup := func() { _ = os.Remove(canary) }
	command := exec.CommandContext(ctx, config.Executable, append(config.Args, descriptorArgument(config.ExtraFiles, false))...)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "TMPDIR=/nonexistent", darwinCanaryEnvironment + "=" + canary}
	command.Dir = "/"
	command.ExtraFiles = config.ExtraFiles
	input, err := command.StdinPipe()
	if err != nil {
		cleanup()
		return nil, ErrUnavailable
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		cleanup()
		return nil, ErrUnavailable
	}
	if err = command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		cleanup()
		return nil, ErrUnavailable
	}
	return newProcess(ctx, input, output, command.Wait, command.Process.Kill, cleanup), nil
}

// The signed app bundle is necessary for libsecinit to create an independent
// sandbox. Inheritance would copy the parent's broader privileges instead.
func darwinSignedWorker(ctx context.Context, executable string) bool {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return false
	}
	const suffix = "/BagelNetworkWorker.app/Contents/MacOS/BagelNetworkWorker"
	if !strings.HasSuffix(resolved, suffix) {
		return false
	}
	bundle := strings.TrimSuffix(resolved, "/Contents/MacOS/BagelNetworkWorker")
	verify := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", "--deep", "-R", `=identifier "`+darwinWorkerIdentifier+`"`, bundle)
	if verify.Run() != nil {
		return false
	}
	display := exec.CommandContext(ctx, "/usr/bin/codesign", "--display", "--entitlements", "-", "--xml", bundle)
	body, err := display.Output()
	if err != nil || len(body) > 4096 {
		return false
	}
	return darwinMinimalEntitlements(body)
}

type darwinWorkerEntitlements struct {
	Dictionary struct {
		Keys    []string   `xml:"key"`
		Enabled []struct{} `xml:"true"`
		Rules   []struct {
			Values []string `xml:"string"`
		} `xml:"array"`
	} `xml:"dict"`
}

func darwinMinimalEntitlements(body []byte) bool {
	if !darwinEntitlementElements(body) {
		return false
	}
	var value darwinWorkerEntitlements
	if xml.Unmarshal(body, &value) != nil {
		return false
	}
	dictionary := value.Dictionary
	if len(dictionary.Keys) != 2 || len(dictionary.Rules) != 1 || len(dictionary.Enabled) != 1 {
		return false
	}
	if !darwinEntitlementKeys(dictionary.Keys) {
		return false
	}
	rules := dictionary.Rules[0].Values
	return len(rules) == 3 && rules[0] == darwinDenyFork && rules[1] == darwinDenyExec && rules[2] == darwinDenyKeychain
}
func darwinEntitlementKeys(keys []string) bool {
	return keys[0] == "com.apple.security.app-sandbox" && keys[1] == "com.apple.security.temporary-exception.sbpl"
}
func darwinEntitlementElements(body []byte) bool {
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	expected := map[string]int{"plist": 1, "dict": 1, "key": 2, "true": 1, "array": 1, "string": 3}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		element, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if element.Name.Space != "" || expected[element.Name.Local] == 0 {
			return false
		}
		expected[element.Name.Local]--
	}
	for _, remaining := range expected {
		if remaining != 0 {
			return false
		}
	}
	return true
}

func darwinCanary() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp(home, ".bagel-worker-probe-*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if _, err = file.WriteString("synthetic isolation probe; contains no account data"); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// ChildIsolated is checked before the worker accepts configuration or secrets.
// It tests actual kernel restrictions, not just an entitlement file. App Sandbox
// also carries fixed deny-only Seatbelt rules, which block subprocesses and
// Keychain Mach services. Unsupported or ignored rules fail these live probes.
func ChildIsolated() bool {
	return darwinHardenedProcess(os.Getpid()) && darwinHomeDenied() && darwinNetworkDenied() && darwinExecDenied() && darwinParentMemoryDenied()
}
func darwinHardenedProcess(pid int) bool {
	var flags uint32
	_, _, errno := syscall.Syscall6(unix.SYS_CSOPS, uintptr(pid), 0, uintptr(unsafe.Pointer(&flags)), unsafe.Sizeof(flags), 0, 0)
	runtime.KeepAlive(&flags)
	const valid = 0x1
	const getTaskAllow = 0x4
	const hardenedRuntime = 0x10000
	const debugged = 0x10000000
	return errno == 0 && flags&valid != 0 && flags&hardenedRuntime != 0 && flags&(getTaskAllow|debugged) == 0
}
func darwinHomeDenied() bool {
	path := os.Getenv(darwinCanaryEnvironment)
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	_, err := os.ReadFile(path)
	return darwinPermissionDenied(err)
}
func darwinNetworkDenied() bool {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if listener != nil {
		_ = listener.Close()
		return false
	}
	if !darwinPermissionDenied(err) {
		return false
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:1", time.Second)
	if conn != nil {
		_ = conn.Close()
		return false
	}
	return darwinPermissionDenied(err)
}
func darwinPermissionDenied(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES)
}
func darwinParentMemoryDenied() (denied bool) {
	defer func() {
		if recover() != nil {
			denied = false
		}
	}()
	library, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return false
	}
	defer purego.Dlclose(library)
	var taskSelf func() uint32
	var taskForPID func(uint32, int32, *uint32) int32
	var deallocate func(uint32, uint32) int32
	purego.RegisterLibFunc(&taskSelf, library, "mach_task_self")
	purego.RegisterLibFunc(&taskForPID, library, "task_for_pid")
	purego.RegisterLibFunc(&deallocate, library, "mach_port_deallocate")
	var task uint32
	self := taskSelf()
	result := taskForPID(self, int32(os.Getppid()), &task)
	if task != 0 {
		_ = deallocate(self, task)
		return false
	}
	return result != 0 && darwinKeychainServicesDenied(library, self)
}
func darwinExecDenied() bool {
	for _, program := range []string{"/bin/sh", "/usr/bin/security"} {
		command := exec.Command(program, "-h")
		if !darwinPermissionDenied(command.Run()) {
			return false
		}
	}
	return true
}
func darwinKeychainServicesDenied(library uintptr, self uint32) bool {
	var special func(uint32, int32, *uint32) int32
	var lookup func(uint32, uintptr, *uint32) int32
	var deallocate func(uint32, uint32) int32
	purego.RegisterLibFunc(&special, library, "task_get_special_port")
	purego.RegisterLibFunc(&lookup, library, "bootstrap_look_up")
	purego.RegisterLibFunc(&deallocate, library, "mach_port_deallocate")
	var bootstrap uint32
	if special(self, 4, &bootstrap) != 0 || bootstrap == 0 {
		return false
	}
	defer deallocate(self, bootstrap)
	for _, service := range []string{"com.apple.securityd.xpc", "com.apple.SecurityServer"} {
		raw := append([]byte(service), 0)
		var port uint32
		result := lookup(bootstrap, uintptr(unsafe.Pointer(&raw[0])), &port)
		runtime.KeepAlive(raw)
		if port != 0 {
			_ = deallocate(self, port)
			return false
		}
		if result != 1100 {
			return false
		}
	}
	return true
}
