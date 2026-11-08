// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build windows

package sandbox

import (
	"bufio"
	"context"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var windowsHelper = flag.Bool("sandbox-platform-helper", false, "run isolated test helper")
var windowsSecret = flag.String("sandbox-parent-secret", "", "private parent fixture path")
var windowsDescriptors = flag.String("sandbox-fds", "", "inherited stream handles")
var windowsLoopback = flag.String("sandbox-parent-loopback", "", "parent test listener")

func TestWindowsInheritanceRejectsFiles(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "private-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if inherited, err := inheritWindowsPipe(file); err == nil {
		_ = inherited.Close()
		t.Fatal("regular files must not cross the sandbox handle boundary")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	for _, file := range []*os.File{reader, writer} {
		inherited, err := inheritWindowsPipe(file)
		if err != nil {
			t.Fatal("explicit pipe handle rejected", err)
		}
		_ = inherited.Close()
	}
}

func TestWindowsKernelSandbox(t *testing.T) {
	if os.Getenv("BAGEL_SANDBOX_KERNEL_TEST") != "1" {
		t.Skip("opt-in native Windows AppContainer kernel validation")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	secret := windowsPrivateFixture(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	process, err := Start(ctx, Config{Executable: executable, Args: []string{"-test.run=^TestWindowsIsolatedHelper$", "--sandbox-platform-helper", "--sandbox-parent-secret=" + secret, "--sandbox-parent-loopback=" + listener.Addr().String()}, ExtraFiles: []*os.File{reader}})
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

func windowsPrivateFixture(t *testing.T) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "parent-private")
	if err := os.WriteFile(name, []byte("must-not-cross-the-boundary"), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestWindowsIsolatedHelper(t *testing.T) {
	if !*windowsHelper {
		t.Skip("isolated helper only")
	}
	if !ChildIsolated() {
		os.Exit(11)
	}
	input, output, err := ControlPipes()
	if err != nil {
		os.Exit(16)
	}
	defer input.Close()
	defer output.Close()
	if _, err := os.ReadFile(*windowsSecret); err == nil {
		os.Exit(12)
	}
	connection, err := net.DialTimeout("tcp", *windowsLoopback, 300*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		os.Exit(13)
	}
	value, err := strconv.ParseUint(strings.Split(*windowsDescriptors, ",")[0], 10, 64)
	if err != nil {
		os.Exit(14)
	}
	stream := os.NewFile(uintptr(value), "broker-stream")
	var payload [12]byte
	if _, err = io.ReadFull(stream, payload[:]); err != nil || string(payload[:]) != "broker-bytes" {
		os.Exit(15)
	}
	_, _ = output.WriteString("isolated\n")
}
