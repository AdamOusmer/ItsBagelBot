// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build !race

package engine

const allocCeiling = 16.0

const allocViewsCeiling = 17.0

const allocFlowReplyCeiling = 48.0

const allocFlowIfCeiling = 48.0

const allocFlowCompareCeiling = 53.0

const allocFlowTwentyStepsCeiling = 55.0

const allocFlowNoMatchCeiling = 18.0
