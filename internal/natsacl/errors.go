// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import "errors"

var (
	ErrUnknownAccount    = errors.New("natsacl: reference to unknown account")
	ErrMissingAccountKey = errors.New("natsacl: missing account key")
	ErrMissingRoleKey    = errors.New("natsacl: missing role key")
	ErrDuplicateRole     = errors.New("natsacl: duplicate role key")
)
