// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build windows

package sandbox

import (
	"runtime"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const securityCapabilitiesAttribute = 0x00020009
const childProcessPolicyAttribute = 0x0002000e

type windowsSecurityCapabilities struct {
	AppContainerSID *windows.SID
	Capabilities    unsafe.Pointer
	CapabilityCount uint32
	Reserved        uint32
}

func (l *windowsLaunch) create(config Config) error {
	job, err := windowsWorkerJob()
	if err != nil {
		return err
	}
	l.job = job
	attributes, err := l.attributes()
	if err != nil {
		return err
	}
	defer attributes.Delete()
	process, err := l.createSuspended(config, attributes)
	if err != nil {
		return err
	}
	l.process = process.Process
	defer windows.CloseHandle(process.Thread)
	if err = l.authorizeProcess(process.Process); err != nil {
		return err
	}
	// Close parent copies while the child is still suspended: os.File.Close
	// calls CancelIoEx, which must not race the child's first overlapped I/O.
	l.closeChildHandles()
	if _, err = windows.ResumeThread(process.Thread); err != nil {
		return err
	}
	return nil
}

func (l *windowsLaunch) attributes() (*windows.ProcThreadAttributeListContainer, error) {
	attributes, err := windows.NewProcThreadAttributeList(3)
	if err != nil {
		return nil, err
	}
	capabilities := &windowsSecurityCapabilities{AppContainerSID: l.profile.sid}
	policy := uint32(1) // PROCESS_CREATION_CHILD_PROCESS_RESTRICTED, never permit descendants.
	handles := make([]windows.Handle, len(l.child))
	for i, file := range l.child {
		handles[i] = windows.Handle(file.Fd())
	}
	if err = attributes.Update(securityCapabilitiesAttribute, unsafe.Pointer(capabilities), unsafe.Sizeof(*capabilities)); err == nil {
		err = attributes.Update(childProcessPolicyAttribute, unsafe.Pointer(&policy), unsafe.Sizeof(policy))
	}
	if err == nil {
		err = attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0]))
	}
	if err != nil {
		attributes.Delete()
		return nil, err
	}
	return attributes, nil
}

func (l *windowsLaunch) createSuspended(config Config, attributes *windows.ProcThreadAttributeListContainer) (*windows.ProcessInformation, error) {
	executable, err := windows.UTF16PtrFromString(l.profile.executable)
	if err != nil {
		return nil, err
	}
	args := append([]string{l.profile.executable}, config.Args...)
	args = append(args, descriptorArgument(l.child[2:], true))
	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return nil, err
	}
	startup := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{
		Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES,
		StdInput: windows.Handle(l.child[0].Fd()), StdOutput: windows.Handle(l.child[1].Fd()), StdErr: windows.Handle(l.child[1].Fd()),
	}, ProcThreadAttributeList: attributes.List()}
	// No inherited environment, user profile path, credential variables or PATH.
	environment := utf16.Encode([]rune("GOMEMLIMIT=96MiB\x00GOMAXPROCS=2\x00\x00"))
	process := new(windows.ProcessInformation)
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	err = windows.CreateProcess(executable, command, nil, nil, true, flags, &environment[0], nil, &startup.StartupInfo, process)
	runtime.KeepAlive(attributes)
	return process, err
}

func (l *windowsLaunch) authorizeProcess(process windows.Handle) error {
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return err
	}
	defer token.Close()
	if !isolatedWindowsToken(token) {
		return ErrUnavailable
	}
	return windows.AssignProcessToJobObject(l.job, process)
}

func windowsWorkerJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{ProcessMemoryLimit: 128 << 20}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY
	limits.BasicLimitInformation.ActiveProcessLimit = 1
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	if err == nil {
		// Hard CPU cap: 20% of machine capacity, outside the event hot path.
		cpu := struct{ Flags, Rate uint32 }{Flags: 1 | 4, Rate: 2000}
		_, err = windows.SetInformationJobObject(job, windows.JobObjectCpuRateControlInformation, uintptr(unsafe.Pointer(&cpu)), uint32(unsafe.Sizeof(cpu)))
	}
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
