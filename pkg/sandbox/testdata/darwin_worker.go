// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// This test-only executable never processes real account data.
package main

import (
	"context"
	"fmt"
	"github.com/ebitengine/purego"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"ItsBagelBot/pkg/sandbox"
)

func main() {
	if !sandbox.ChildIsolated() {
		fmt.Fprintln(os.Stdout, "isolation-failed")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "isolated")
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "--keychain-probe=") {
		service := strings.TrimPrefix(os.Args[1], "--keychain-probe=")
		keychainProbe(service)
		keychainCLIProbe(service)
		return
	}
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[len(os.Args)-1], "--sandbox-fds=3") {
		stream, err := sandbox.OpenPipe(3, "synthetic-stream")
		if err != nil {
			fmt.Fprintln(os.Stdout, "pipe-adoption-failed")
			return
		}
		defer stream.Close()
		if err := stream.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			fmt.Fprintln(os.Stdout, "deadline-unsupported")
			return
		}
		_, _ = io.Copy(os.Stdout, stream)
		return
	}
	_, _ = io.Copy(os.Stdout, os.Stdin)
}

func keychainProbe(service string) {
	library, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		fmt.Println("keychain-probe-unavailable")
		return
	}
	defer purego.Dlclose(library)
	var ui func(bool) int32
	purego.RegisterLibFunc(&ui, library, "SecKeychainSetUserInteractionAllowed")
	ui(false)
	var find func(uintptr, uint32, uintptr, uint32, uintptr, *uint32, *uintptr, uintptr) int32
	purego.RegisterLibFunc(&find, library, "SecKeychainFindGenericPassword")
	var free func(uintptr, uintptr) int32
	purego.RegisterLibFunc(&free, library, "SecKeychainItemFreeContent")
	raw := append([]byte(service), 0)
	account := []byte("probe\x00")
	var count uint32
	var value uintptr
	status := find(0, uint32(len(service)), uintptr(unsafe.Pointer(&raw[0])), 5, uintptr(unsafe.Pointer(&account[0])), &count, &value, 0)
	if value != 0 {
		free(0, value)
	}
	if status != 0 && count == 0 {
		fmt.Println("keychain-denied")
	} else {
		fmt.Println("keychain-accessible")
	}
}

func keychainCLIProbe(service string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-a", "probe", "-s", service, "-w")
	command.Env = []string{"HOME=" + filepath.Dir(os.Getenv("BAGEL_SANDBOX_CANARY")), "PATH=/usr/bin:/bin"}
	value, err := command.Output()
	if ctx.Err() != nil {
		fmt.Println("keychain-cli-probe-unavailable")
		return
	}
	if err != nil && len(value) == 0 {
		fmt.Println("keychain-cli-denied")
	} else {
		fmt.Println("keychain-cli-accessible")
	}
}
