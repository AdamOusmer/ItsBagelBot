// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build linux && !amd64 && !arm64

package sandbox

import "context"

func platformStart(context.Context, Config) (*Process, error) { return nil, ErrUnavailable }
func ChildIsolated() bool                                     { return false }
