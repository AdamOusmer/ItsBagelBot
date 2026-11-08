// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build darwin || linux

package sandbox

import (
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOpenPipeRegistersInheritedBlockingDescriptor(t *testing.T) {
	reader, writer, err := Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	// Duplicate the descriptor like exec. Calling
	// File.Fd deliberately restores blocking mode before the new process adopts it.
	fd, err := unix.Dup(int(reader.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := OpenPipe(uintptr(fd), "inherited-test-pipe")
	if err != nil {
		_ = unix.Close(fd)
		t.Fatal(err)
	}
	defer adopted.Close()
	if err = adopted.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal("adopted pipe has no deadline support", err)
	}
	if _, err = writer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	var value [1]byte
	if _, err = adopted.Read(value[:]); err != nil || value[0] != 'x' {
		t.Fatalf("pipe read failed: %v", err)
	}
}
func TestOpenPipeRejectsRegularFilesAndStandardDescriptors(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "not-a-stream")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fd, err := unix.Dup(int(file.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = OpenPipe(uintptr(fd), "credential-file")
	if !errors.Is(err, ErrConfiguration) {
		t.Fatal("regular file accepted", err)
	}
	if _, err = file.WriteString("still caller owned"); err != nil {
		t.Fatal("validation failure closed unrelated original file", err)
	}
	_, err = OpenPipe(0, "standard-input")
	if !errors.Is(err, ErrConfiguration) {
		t.Fatal("standard descriptor accepted", err)
	}
}
