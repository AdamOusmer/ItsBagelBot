// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { VariableChip } from './surfaces';

const VARS = 'vars.';
const REPLY_VARS = 'replyVars.';

export function chipHint(t: (key: string) => string, chip: VariableChip): string | undefined {
  const key = chip.hintKey;
  if (key?.startsWith(VARS)) return t(`vars.${key.slice(VARS.length)}`);
  if (key?.startsWith(REPLY_VARS)) return t(`replyVars.${key.slice(REPLY_VARS.length)}`);
  return undefined;
}
