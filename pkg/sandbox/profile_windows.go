// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build windows

package sandbox

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var userenv = windows.NewLazySystemDLL("userenv.dll")
var createAppContainer = userenv.NewProc("CreateAppContainerProfile")
var deleteAppContainer = userenv.NewProc("DeleteAppContainerProfile")

type windowsProfile struct {
	name, directory, executable string
	sid                         *windows.SID
}

func newWindowsProfile(executable string) (*windowsProfile, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	profile := &windowsProfile{name: "ItsBagelBot.worker." + hex.EncodeToString(nonce[:])}
	if err := profile.create(); err != nil {
		return nil, err
	}
	if err := profile.stage(executable); err != nil {
		profile.close()
		return nil, err
	}
	return profile, nil
}

func (p *windowsProfile) create() error {
	if createAppContainer.Find() != nil || deleteAppContainer.Find() != nil {
		return ErrUnavailable
	}
	name, err := windows.UTF16PtrFromString(p.name)
	if err != nil {
		return err
	}
	result, _, _ := createAppContainer.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0, 0, uintptr(unsafe.Pointer(&p.sid)))
	if int32(result) < 0 || p.sid == nil {
		return ErrUnavailable
	}
	return nil
}

func (p *windowsProfile) stage(source string) error {
	directory, err := os.MkdirTemp("", "ItsBagelBot-worker-")
	if err != nil {
		return err
	}
	p.directory = directory
	if err = protectWindowsWorker(directory, p.sid); err != nil {
		return err
	}
	p.executable = filepath.Join(directory, "worker.exe")
	return copyWindowsWorker(source, p.executable)
}

func protectWindowsWorker(directory string, sid *windows.SID) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	// The protected directory grants this user/System full access; the unique
	// zero-capability AppContainer receives read/execute only, inherited by its exe.
	sddl := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;GRGX;;;" + sid.String() + ")"
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	err = windows.SetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	runtime.KeepAlive(descriptor)
	return err
}

func copyWindowsWorker(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<20 {
		return ErrConfiguration
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0500)
	if err != nil {
		return err
	}
	_, copyErr := io.CopyN(output, input, info.Size())
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (p *windowsProfile) close() {
	if p.directory != "" {
		_ = os.RemoveAll(p.directory)
	}
	if name, err := windows.UTF16PtrFromString(p.name); err == nil {
		_, _, _ = deleteAppContainer.Call(uintptr(unsafe.Pointer(name)))
	}
	if p.sid != nil {
		_ = windows.FreeSid(p.sid)
	}
}
