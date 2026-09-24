// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { VARIABLES } from './variables';

export type VariableHintKey = `vars.${string}.hint`;

export const HINT_KEYS: Readonly<Record<string, VariableHintKey>> = Object.fromEntries(
  VARIABLES.map((v) => [v.id, `vars.${v.id}.hint` as const])
);
