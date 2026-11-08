//go:build !darwin && !linux

// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package compare

import "time"

func clientCPU() time.Duration { return 0 }
