//go:build darwin || linux

// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package compare

import (
	"syscall"
	"time"
)

// Only this process's scheduled user+kernel CPU; the test server is a child
// process and therefore does not contribute to RUSAGE_SELF.
func clientCPU() time.Duration {
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
		return 0
	}
	return time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second + time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond
}
