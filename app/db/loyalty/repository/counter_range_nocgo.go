//go:build !cgo

// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

// Production uses MySQL and disables CGO; SQLite is a test-only driver.
func sqliteCounterRangeError(error) bool { return false }
