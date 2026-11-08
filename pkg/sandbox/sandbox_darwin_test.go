// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build darwin

package sandbox

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const darwinTestEntitlements = `<?xml version="1.0"?><plist version="1.0"><dict><key>com.apple.security.app-sandbox</key><true/><key>com.apple.security.temporary-exception.sbpl</key><array><string>` + darwinDenyFork + `</string><string>` + darwinDenyExec + `</string><string>` + darwinDenyKeychain + `</string></array></dict></plist>`

func darwinTestBundle(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bundle := filepath.Join(dir, "BagelNetworkWorker.app")
	mac := filepath.Join(bundle, "Contents", "MacOS")
	if err := os.MkdirAll(mac, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(mac, "BagelNetworkWorker")
	build := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", executable, "./testdata/darwin_worker.go")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("fixture build: %v: %s", err, out)
	}
	info := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>` + darwinWorkerIdentifier + `</string><key>CFBundleExecutable</key><string>BagelNetworkWorker</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(info), 0600); err != nil {
		t.Fatal(err)
	}
	return bundle, executable
}
func darwinSignTestBundle(t *testing.T, bundle, entitlements string) {
	t.Helper()
	path := filepath.Join(filepath.Dir(bundle), "probe.entitlements")
	if err := os.WriteFile(path, []byte(entitlements), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "/usr/bin/codesign", "--force", "--sign", "-", "--options", "runtime", "--entitlements", path, bundle)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture signature: %v: %s", err, out)
	}
}
func TestDarwinSignedSandboxRoundTrip(t *testing.T) {
	bundle, executable := darwinTestBundle(t)
	darwinSignTestBundle(t, bundle, darwinTestEntitlements)
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	process, err := Start(ctx, Config{Executable: executable, ExtraFiles: []*os.File{read}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	output := bufio.NewReader(process.Output)
	hello, err := output.ReadString('\n')
	if err != nil || hello != "isolated\n" {
		t.Fatalf("live isolation probe: %q %v", hello, err)
	}
	if _, err = write.WriteString("synthetic encrypted-stream placeholder\n"); err != nil {
		t.Fatal(err)
	}
	_ = write.Close()
	line, err := output.ReadString('\n')
	if err != nil || line != "synthetic encrypted-stream placeholder\n" {
		t.Fatalf("inherited pipe: %q %v", line, err)
	}
	if err = process.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("signed helper: hardened runtime; home/network/exec/Keychain-Mach/parent-memory denied; inherited pipe roundtrip passed")
}
func TestDarwinRejectsUnsignedAndOverprivilegedHelper(t *testing.T) {
	bundle, executable := darwinTestBundle(t)
	_, err := Start(t.Context(), Config{Executable: executable})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unsigned helper accepted: %v", err)
	}
	elevated := strings.Replace(darwinTestEntitlements, "</dict>", "<key>com.apple.security.network.client</key><true/></dict>", 1)
	darwinSignTestBundle(t, bundle, elevated)
	_, err = Start(t.Context(), Config{Executable: executable})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("network entitlement accepted: %v", err)
	}
}
func TestDarwinMinimalEntitlements(t *testing.T) {
	if !darwinMinimalEntitlements([]byte(darwinTestEntitlements)) {
		t.Fatal("minimal entitlement rejected")
	}
	production, err := os.ReadFile("../../app/desktop/build/darwin/NetworkWorker.entitlements")
	if err != nil || !darwinMinimalEntitlements(production) {
		t.Fatal("production worker entitlements differ from the fixed policy")
	}
	weak := `<?xml version="1.0"?><plist version="1.0"><dict><key>com.apple.security.app-sandbox</key><true/></dict></plist>`
	if darwinMinimalEntitlements([]byte(weak)) {
		t.Fatal("App Sandbox without fixed deny layer accepted")
	}

	for _, extra := range []string{"<key>com.apple.security.inherit</key><true/>", "<key>keychain-access-groups</key><array><string>shared</string></array>", "<key>com.apple.security.get-task-allow</key><true/>"} {
		value := strings.Replace(darwinTestEntitlements, "</dict>", extra+"</dict>", 1)
		if darwinMinimalEntitlements([]byte(value)) {
			t.Fatal("additional privileges accepted")
		}
	}
	if ChildIsolated() {
		t.Fatal("unsandboxed test process falsely attested")
	}
}

// This opt-in test creates and removes only a uniquely named synthetic item.
// The helper disables Keychain UI before probing: it cannot ask the user to
// override the boundary, and never prints item contents.
func TestDarwinSyntheticDefaultKeychainDenied(t *testing.T) {
	if os.Getenv("BAGEL_SANDBOX_KEYCHAIN_PROBE") != "1" {
		t.Skip("opt-in synthetic default-keychain probe")
	}
	service := darwinTestKeychainItem(t)
	bundle, executable := darwinTestBundle(t)
	darwinSignTestBundle(t, bundle, darwinTestEntitlements)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	process, err := Start(ctx, Config{Executable: executable, Args: []string{"--keychain-probe=" + service}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	output := bufio.NewReader(process.Output)
	hello, err := output.ReadString('\n')
	if err != nil || hello != "isolated\n" {
		t.Fatalf("live isolation check failed: %q %v", hello, err)
	}
	result, err := output.ReadString('\n')
	if err != nil || result != "keychain-denied\n" {
		t.Fatalf("synthetic keychain restriction: %q %v", result, err)
	}
	cli, err := output.ReadString('\n')
	if err != nil || cli != "keychain-cli-denied\n" {
		t.Fatalf("inherited child CLI keychain restriction: %q %v", cli, err)
	}
	if err = process.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("synthetic default-keychain item: parent readable; isolated child denied without UI; fixture removed by cleanup")
}

func darwinTestKeychainItem(t *testing.T) string {
	t.Helper()
	service := darwinWorkerIdentifier + ".synthetic." + strconv.FormatInt(time.Now().UnixNano(), 10)
	create := exec.CommandContext(t.Context(), "/usr/bin/security", "add-generic-password", "-a", "probe", "-s", service, "-w", "synthetic-not-a-real-credential")
	if err := create.Run(); err != nil {
		t.Fatal("synthetic default-keychain fixture unavailable; no unlock attempted")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "/usr/bin/security", "delete-generic-password", "-a", "probe", "-s", service).Run(); err != nil {
			t.Error("failed to delete owned synthetic keychain fixture")
		}
	})
	baseline := exec.CommandContext(t.Context(), "/usr/bin/security", "find-generic-password", "-a", "probe", "-s", service, "-w")
	if body, err := baseline.Output(); err != nil || len(body) == 0 {
		t.Fatal("synthetic fixture baseline not readable")
	}

	return service
}
