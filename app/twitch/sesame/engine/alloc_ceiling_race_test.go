// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

//go:build race

package engine

const allocCeiling = 20.0

const allocViewsCeiling = 21.0

const allocFlowReplyCeiling = 61.0

const allocFlowIfCeiling = 61.0

const allocFlowCompareCeiling = 65.0

const allocFlowTwentyStepsCeiling = 79.0

const allocFlowNoMatchCeiling = 20.0

const allocFlowRefusedCeiling = 20.0
