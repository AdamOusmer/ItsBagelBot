// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export * from './types';
export * from './variables';
export * from './preview-values';
export * from './surfaces';
export * from './hint-keys';
import { VARIABLES } from './variables';
import type { VariableDef } from './types';

const BY_ID = new Map<string, VariableDef>(VARIABLES.map((v) => [v.id, v]));

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
