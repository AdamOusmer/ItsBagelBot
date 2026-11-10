// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

// BatchDelete deletes the oldest rows of Table whose Column is below the bound, at most the limit per statement.
// Both placeholders are the bound then the limit. Inclusive compares with <= instead of <.
type BatchDelete struct {
	Table     string
	Column    string
	Inclusive bool
}

func (b BatchDelete) Query(dialectName string) string {
	op := "<"
	if b.Inclusive {
		op = "<="
	}
	tail := " WHERE " + b.Column + op + "? ORDER BY " + b.Column + " LIMIT ?"
	if dialectName == "sqlite3" {
		return "DELETE FROM " + b.Table + " WHERE id IN (SELECT id FROM " + b.Table + tail + ")"
	}
	return "DELETE FROM " + b.Table + tail
}
