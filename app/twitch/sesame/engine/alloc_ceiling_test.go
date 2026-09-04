// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build !race

package engine

// allocCeiling is the per-Process allocation limit used by
// TestProcessNoOutputAllocCeiling.  Without the race detector the hot path sits
// at 12 allocs (the JSON decoder floor); keep the ceiling tight so any
// structural regression (un-pooling Context/Envelope) is caught immediately.
const allocCeiling = 12.0

// allocViewsCeiling bounds the automod-wired shape of the same hot path
// (TestProcessWithViewsAllocCeiling). Measured at 12 allocs/op after the
// ModuleView map moved into viewsPool (was 14 when the map was rebuilt per
// line, M1 Pro, 2026-08-22); one alloc of headroom absorbs decoder variance
// without hiding a structural regression.
const allocViewsCeiling = 13.0
