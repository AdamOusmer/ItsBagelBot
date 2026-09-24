// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type VariableGroup = 'who' | 'typed' | 'stream' | 'fun' | 'data';

export interface VariableForm {
  readonly syntax: string;
  readonly example: string;
  readonly output: string;
  readonly chipHint?: string;
}

export interface VariableDef {
  readonly id: string;
  readonly head: string;
  readonly group: VariableGroup;
  readonly forms: readonly VariableForm[];
  readonly aliases?: readonly string[];
  readonly legacy?: boolean;
  readonly requires?: string | null;
  readonly pinned?: boolean;
}
