// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
export const DOC_TYPES = ['tutorial', 'how-to', 'reference', 'explanation', 'decision', 'record'] as const;
export type DocType = (typeof DOC_TYPES)[number];
