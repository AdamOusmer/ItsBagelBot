// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build !darwin && !linux && !windows

package sandbox

import (
	"context"
	"os"
)

func platformStart(context.Context, Config) (*Process, error) { return nil, ErrUnavailable }
func ChildIsolated() bool                                     { return false }
func Pipe() (*os.File, *os.File, error)                       { return nil, nil, ErrUnavailable }
func OpenPipe(uintptr, string) (*os.File, error)              { return nil, ErrUnavailable }
func ControlPipes() (*os.File, *os.File, error)               { return nil, nil, ErrUnavailable }
