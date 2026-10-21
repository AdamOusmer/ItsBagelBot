// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import "strings"

// SQLite words FOR UPDATE refusals differently per ent query method: IDs() fails parsing, All() and Only() are refused by ent.
func RowLocksUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, `near "FOR"`) || strings.Contains(msg, "FOR UPDATE/SHARE not supported in SQLite")
}
