// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build race

package engine

// allocCeiling is the per-Process allocation limit used by
// TestProcessNoOutputAllocCeiling.  The race detector instruments sync.Pool and
// context operations, adding 2-3 allocations that do not exist in production
// builds.  The wider ceiling keeps the test useful as a structural regression
// guard without flaking under -race.
const allocCeiling = 16.0
