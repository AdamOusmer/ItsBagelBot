// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { normalizeCounterName } from '@bagel/kit/validation';

/** The draft the counters inspector edits, for both an existing row and a new one. */
export type CounterDraft = {
  name: string;
  value: number;
};

export const NEW_COUNTER = '__new__';

export function blankCounter(): CounterDraft {
  return { name: '', value: 0 };
}

/**
 * Whether `draft` carries enough to post. Mirrors the create/set actions' own
 * validation -- normalizeCounterName then a non-empty, colon-free name -- so
 * Save is not offered for a body the server will answer 400 to. ':' is the
 * worker's bot-token prefix and is reserved, which is why it is rejected here
 * rather than folded away.
 */
export function counterComplete(draft: CounterDraft): boolean {
  const name = normalizeCounterName(draft.name);
  if (!name || name.includes(':')) return false;
  return Number.isFinite(draft.value);
}
