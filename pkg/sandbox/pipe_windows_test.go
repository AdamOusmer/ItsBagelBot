// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build windows

package sandbox

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsOverlappedPipeDeadlines(t *testing.T) {
	reader, writer, err := Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if err = reader.SetReadDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read not bounded: %v", err)
	}
	if err = reader.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	var body [1]byte
	if _, err = io.ReadFull(reader, body[:]); err != nil || body[0] != 'a' {
		t.Fatalf("%q %v", body, err)
	}
}
func TestWindowsInheritedOverlappedPipeAdoption(t *testing.T) {
	reader, writer, err := Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	inherited, err := inheritWindowsPipe(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Fd detaches the temporary launcher wrapper from IOCP. Duplicate once more
	// to model transfer without retaining two os.File owners of the same handle.
	var duplicate windows.Handle
	err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(inherited.Fd()), windows.CurrentProcess(), &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS)
	_ = inherited.Close()
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := OpenPipe(uintptr(duplicate), "child-read")
	if err != nil {
		t.Fatal(err)
	}
	defer adopted.Close()
	if err = adopted.SetReadDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err = adopted.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("inherited read not bounded: %v", err)
	}
}
func TestWindowsOpenPipeRejectsSynchronousHandles(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	handle := reader.Fd()
	var duplicate windows.Handle
	err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(handle), windows.CurrentProcess(), &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if file, err := OpenPipe(uintptr(duplicate), "blocking-pipe"); err == nil {
		_ = file.Close()
		t.Fatal("uninterruptible synchronous handle accepted")
	}
}
