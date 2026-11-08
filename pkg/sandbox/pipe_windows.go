// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build windows

package sandbox

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Pipe creates private, connected, one-way pipe handles with overlapped I/O.
// Unlike Windows os.Pipe/CreatePipe, both ends support Go deadlines. The name
// is random, restricted to this user/System, local-only and single-instance.
// https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-createnamedpipea
func Pipe() (*os.File, *os.File, error) {
	reader, writer, err := windowsPipeHandles()
	if err != nil {
		return nil, nil, err
	}
	r, err := OpenPipe(uintptr(reader), "sandbox-pipe-read")
	if err != nil {
		_ = windows.CloseHandle(writer)
		return nil, nil, err
	}
	w, err := OpenPipe(uintptr(writer), "sandbox-pipe-write")
	if err != nil {
		_ = r.Close()
		return nil, nil, err
	}
	return r, w, nil
}

func windowsPipeHandles() (windows.Handle, windows.Handle, error) {
	name, security, err := windowsPipeConfiguration()
	if err != nil {
		return 0, 0, err
	}
	flags := uint32(windows.PIPE_ACCESS_INBOUND | windows.FILE_FLAG_OVERLAPPED | windows.FILE_FLAG_FIRST_PIPE_INSTANCE)
	reader, err := windows.CreateNamedPipe(name, flags, windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 64<<10, 64<<10, 0, security)
	runtime.KeepAlive(security)
	if err != nil {
		return 0, 0, err
	}
	writer, err := windows.CreateFile(name, windows.GENERIC_WRITE|windows.FILE_READ_ATTRIBUTES, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		_ = windows.CloseHandle(reader)
		return 0, 0, err
	}
	if err = connectWindowsPipe(reader, writer); err != nil {
		_ = windows.CloseHandle(reader)
		_ = windows.CloseHandle(writer)
		return 0, 0, err
	}
	return reader, writer, nil
}

func windowsPipeConfiguration() (*uint16, *windows.SecurityAttributes, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, nil, err
	}
	name, err := windows.UTF16PtrFromString(`\\.\pipe\LOCAL\bagel-worker-` + hex.EncodeToString(random[:]))
	if err != nil {
		return nil, nil, err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, nil, err
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;SY)(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, nil, err
	}
	return name, &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}, nil
}

// The client is already connected, so ERROR_PIPE_CONNECTED is success. An
// unexpected pending operation is canceled and joined before its storage dies.
func connectWindowsPipe(reader, writer windows.Handle) error {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	operation := windows.Overlapped{HEvent: event}
	err = windows.ConnectNamedPipe(reader, &operation)
	if err == windows.ERROR_IO_PENDING {
		_ = windows.CancelIoEx(reader, &operation)
		var transferred uint32
		_ = windows.GetOverlappedResult(reader, &operation, &transferred, true)
		return ErrUnavailable
	}
	if err != nil && err != windows.ERROR_PIPE_CONNECTED {
		return err
	}
	var client, server uint32
	if windows.GetNamedPipeClientProcessId(reader, &client) != nil || windows.GetNamedPipeServerProcessId(writer, &server) != nil || client != uint32(os.Getpid()) || server != uint32(os.Getpid()) {
		return ErrUnavailable
	}
	return nil
}

// OpenPipe adopts an inherited pipe handle only if the runtime can poll it.
// It owns fd >= 3 on entry, including failure. Standard descriptors are
// rejected without closing. Never use sockets or ordinary files.
func OpenPipe(fd uintptr, name string) (*os.File, error) {
	if fd < 3 {
		return nil, ErrConfiguration
	}
	handle := windows.Handle(fd)
	if kind, err := windows.GetFileType(handle); err != nil || kind != windows.FILE_TYPE_PIPE || windows.GetNamedPipeInfo(handle, nil, nil, nil, nil) != nil {
		_ = windows.CloseHandle(handle)
		return nil, ErrConfiguration
	}
	file := os.NewFile(fd, name)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, ErrConfiguration
	}
	if file.SetDeadline(time.Time{}) != nil {
		_ = file.Close()
		return nil, ErrUnavailable
	}
	return file, nil
}

// ControlPipes duplicates inherited standard pipes before adoption. Go treats
// the original standard-input handle as synchronous even when OVERLAPPED, so
// the worker must use these owned copies instead of the os.Stdin wrapper.
func ControlPipes() (*os.File, *os.File, error) {
	input, err := duplicateControlPipe(os.Stdin, "sandbox-control-input")
	if err != nil {
		return nil, nil, err
	}
	output, err := duplicateControlPipe(os.Stdout, "sandbox-control-output")
	if err != nil {
		_ = input.Close()
		return nil, nil, err
	}
	return input, output, nil
}
func duplicateControlPipe(file *os.File, name string) (*os.File, error) {
	// Fd explicitly detaches any Go-created standard-output IOCP registration
	// before the duplicate is associated with its sole active runtime wrapper.
	handle := windows.Handle(file.Fd())
	if kind, err := windows.GetFileType(handle); err != nil || kind != windows.FILE_TYPE_PIPE || windows.GetNamedPipeInfo(handle, nil, nil, nil, nil) != nil {
		return nil, ErrConfiguration
	}
	var duplicate windows.Handle
	process := windows.CurrentProcess()
	if err := windows.DuplicateHandle(process, handle, process, &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, ErrUnavailable
	}
	return OpenPipe(uintptr(duplicate), name)
}
