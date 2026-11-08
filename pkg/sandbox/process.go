// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package sandbox owns fail-closed OS process isolation. Application protocols,
// credentials, network brokering and worker supervision belong to the caller.
package sandbox

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var ErrUnavailable = errors.New("sandbox: required OS isolation unavailable")
var ErrConfiguration = errors.New("sandbox: invalid worker configuration")

type Config struct {
	Executable string
	Args       []string
	// ExtraFiles are explicit raw-stream pipes, never files containing credentials.
	ExtraFiles []*os.File
}
type Process struct {
	Input  io.WriteCloser
	Output io.ReadCloser
	done   chan struct{}
	kill   func() error
	err    error
	once   sync.Once
}

func Start(ctx context.Context, config Config) (*Process, error) {
	if ctx == nil || !filepath.IsAbs(config.Executable) || len(config.ExtraFiles) > 8 {
		return nil, ErrConfiguration
	}
	info, err := os.Stat(config.Executable)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrConfiguration
	}
	for _, f := range config.ExtraFiles {
		if f == nil {
			return nil, ErrConfiguration
		}
		info, err := f.Stat()
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			return nil, ErrConfiguration
		}
	}
	return platformStart(ctx, config)
}

// newProcess adapts native process handles as well as exec.Cmd. wait owns the
// sole OS wait; cleanup runs only after exit. Context cancellation kills the
// worker rather than silently replacing it with an unsandboxed process.
func newProcess(ctx context.Context, input io.WriteCloser, output io.ReadCloser, wait, kill func() error, cleanup func()) *Process {
	p := &Process{Input: input, Output: output, kill: kill, done: make(chan struct{})}
	go func() {
		p.err = wait()
		if cleanup != nil {
			cleanup()
		}
		close(p.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = p.Close()
		case <-p.done:
		}
	}()
	return p
}
func (p *Process) Wait(ctx context.Context) error {
	select {
	case <-p.done:
		return p.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *Process) Close() error {
	var err error
	p.once.Do(func() { err = p.kill(); _ = p.Input.Close(); _ = p.Output.Close() })
	return err
}
func descriptorArgument(files []*os.File, windows bool) string {
	values := make([]string, len(files))
	for i, file := range files {
		value := uintptr(i + 3)
		if windows {
			value = file.Fd()
		}
		values[i] = strconv.FormatUint(uint64(value), 10)
	}
	return "--sandbox-fds=" + strings.Join(values, ",")
}
