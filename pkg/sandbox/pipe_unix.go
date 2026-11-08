// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build darwin || linux

package sandbox

import (
	"os"

	"golang.org/x/sys/unix"
)

// OpenPipe owns a nonstandard descriptor on entry, including on failure. It
// never closes standard descriptors rejected by validation. exec obtains descriptors
// through File.Fd, which restores blocking mode; enable nonblocking mode before
// NewFile so Go registers the pipe with its poller and supports deadlines.
func OpenPipe(fd uintptr, name string) (*os.File, error) {
	if fd < 3 || fd > uintptr(^uint(0)>>1) {
		return nil, ErrConfiguration
	}
	var state unix.Stat_t
	if unix.Fstat(int(fd), &state) != nil || state.Mode&unix.S_IFMT != unix.S_IFIFO {
		_ = unix.Close(int(fd))
		return nil, ErrConfiguration
	}
	if unix.SetNonblock(int(fd), true) != nil {
		_ = unix.Close(int(fd))
		return nil, ErrUnavailable
	}
	file := os.NewFile(fd, name)
	if file == nil {
		_ = unix.Close(int(fd))
		return nil, ErrUnavailable
	}
	return file, nil
}

// Pipe creates the broker's raw stream channel without exposing platform APIs
// to application code. The child re-adopts its end with OpenPipe after exec.
func Pipe() (*os.File, *os.File, error) { return os.Pipe() }

// ControlPipes adopts pollable duplicates of standard input and output without
// closing or changing ownership of the original standard descriptors.
func ControlPipes() (*os.File, *os.File, error) {
	input, err := duplicateControlPipe(0, "worker-control-input")
	if err != nil {
		return nil, nil, err
	}
	output, err := duplicateControlPipe(1, "worker-control-output")
	if err != nil {
		_ = input.Close()
		return nil, nil, err
	}
	return input, output, nil
}
func duplicateControlPipe(original int, name string) (*os.File, error) {
	fd, err := unix.Dup(original)
	if err != nil {
		return nil, ErrUnavailable
	}
	unix.CloseOnExec(fd)
	return OpenPipe(uintptr(fd), name)
}
