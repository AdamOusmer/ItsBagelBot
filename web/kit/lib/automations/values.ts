// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const CMP_OPS = ['eq', 'ne', 'gt', 'gte', 'lt', 'lte'];
export const PERM_LEVELS = ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'];
export const VAR_OPS_WITH_VALUE: readonly string[] = [...CMP_OPS, 'contains', 'starts'];
export const VAR_OPS = [...VAR_OPS_WITH_VALUE, 'exists', 'not_exists'];
