// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build !race

package engine

// allocCeiling is the per-Process allocation limit used by
// TestProcessNoOutputAllocCeiling.  Without the race detector the hot path sits
// at 12 allocs (the JSON decoder floor); keep the ceiling tight so any
// structural regression (un-pooling Context/Envelope) is caught immediately.
const allocCeiling = 12.0
