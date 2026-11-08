// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build windows

package sandbox

import (
	"context"
	"encoding/binary"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsLaunch struct {
	mu            sync.Mutex
	profile       *windowsProfile
	input, output *os.File
	child         []*os.File
	process, job  windows.Handle
}

// AppContainer's kernel token, not low integrity or a job alone, enforces the
// user-file/credential/network boundary. The job bounds resources and lifetime.
// https://learn.microsoft.com/en-us/windows/win32/secauthz/implementing-an-appcontainer
func platformStart(ctx context.Context, config Config) (*Process, error) {
	profile, err := newWindowsProfile(config.Executable)
	if err != nil {
		return nil, ErrUnavailable
	}
	launch := &windowsLaunch{profile: profile}
	started := false
	defer func() {
		if !started {
			launch.failed()
		}
	}()
	if err = launch.pipes(config.ExtraFiles); err != nil {
		return nil, ErrUnavailable
	}
	if err = launch.create(config); err != nil {
		return nil, ErrUnavailable
	}
	started = true
	return newProcess(ctx, launch.input, launch.output, launch.wait, launch.kill, launch.cleanup), nil
}

func (l *windowsLaunch) pipes(extra []*os.File) error {
	input, err := l.inputPipe()
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := l.outputPipe()
	if err != nil {
		return err
	}
	defer output.Close()
	for _, file := range append([]*os.File{input, output}, extra...) {
		inherited, err := inheritWindowsPipe(file)
		if err != nil {
			return err
		}
		l.child = append(l.child, inherited)
	}
	return nil
}

func (l *windowsLaunch) inputPipe() (*os.File, error) {
	reader, writer, err := Pipe()
	if err == nil {
		l.input = writer
	}
	return reader, err
}

func (l *windowsLaunch) outputPipe() (*os.File, error) {
	reader, writer, err := Pipe()
	if err == nil {
		l.output = reader
	}
	return writer, err
}

func inheritWindowsPipe(file *os.File) (*os.File, error) {
	handle := windows.Handle(file.Fd())
	kind, err := windows.GetFileType(handle)
	if err != nil || kind != windows.FILE_TYPE_PIPE || windows.GetNamedPipeInfo(handle, nil, nil, nil, nil) != nil {
		return nil, ErrConfiguration
	}
	var inherited windows.Handle
	process := windows.CurrentProcess()
	if err = windows.DuplicateHandle(process, handle, process, &inherited, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(inherited), "sandbox-stream"), nil
}

func (l *windowsLaunch) closeChildHandles() {
	for _, file := range l.child {
		_ = file.Close()
	}
	l.child = nil
}

func (l *windowsLaunch) wait() error {
	if _, err := windows.WaitForSingleObject(l.process, windows.INFINITE); err != nil {
		return ErrUnavailable
	}
	var code uint32
	if windows.GetExitCodeProcess(l.process, &code) != nil || code != 0 {
		return ErrUnavailable
	}
	return nil
}

func (l *windowsLaunch) kill() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.process == 0 {
		return nil
	}
	if err := windows.TerminateProcess(l.process, 1); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (l *windowsLaunch) failed() {
	_ = l.kill()
	if l.process != 0 {
		_, _ = windows.WaitForSingleObject(l.process, 5000)
	}
	if l.input != nil {
		_ = l.input.Close()
	}
	if l.output != nil {
		_ = l.output.Close()
	}
	l.cleanup()
}

func (l *windowsLaunch) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closeChildHandles()
	if l.process != 0 {
		_ = windows.CloseHandle(l.process)
		l.process = 0
	}
	if l.job != 0 {
		_ = windows.CloseHandle(l.job)
		l.job = 0
	}
	l.profile.close()
}

// ChildIsolated queries the actual kernel token before any worker bootstrap.
// Windows enum values 29/30 are TokenIsAppContainer/TokenCapabilities.
// https://learn.microsoft.com/en-us/windows/win32/api/winnt/ne-winnt-token_information_class
func ChildIsolated() bool { return isolatedWindowsToken(windows.GetCurrentProcessToken()) }

func isolatedWindowsToken(token windows.Token) bool {
	var isolated uint32
	var size uint32
	if windows.GetTokenInformation(token, 29, (*byte)(unsafe.Pointer(&isolated)), 4, &size) != nil || isolated != 1 {
		return false
	}
	var capabilities [64]byte
	if windows.GetTokenInformation(token, 30, &capabilities[0], uint32(len(capabilities)), &size) != nil || size < 4 {
		return false
	}
	return binary.LittleEndian.Uint32(capabilities[:4]) == 0
}
