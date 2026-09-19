// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Public surface of the variables manifest (docs/specs/variables-catalog.md
// D2: kit is the shared data home). Re-exports plus the two lookups callers
// actually reach for, Map-backed so both are O(1) rather than a re-scan of
// VARIABLES on every call.

export * from './types';
export * from './variables';
export * from './samples';
export * from './surfaces';
import { VARIABLES } from './variables';
import type { VariableDef } from './types';

const BY_ID = new Map<string, VariableDef>(VARIABLES.map((v) => [v.id, v]));

/** Every head or alias resolves to its owning VariableDef, so a caller
 * looking a lexer head up (chip highlighting, an importer's parity check)
 * never has to special-case aliases. */
const BY_HEAD = new Map<string, VariableDef>();
for (const v of VARIABLES) {
  BY_HEAD.set(v.head, v);
  for (const alias of v.aliases ?? []) BY_HEAD.set(alias, v);
}

export function variableById(id: string): VariableDef | undefined {
  return BY_ID.get(id);
}

export function variableByHead(head: string): VariableDef | undefined {
  return BY_HEAD.get(head);
}
