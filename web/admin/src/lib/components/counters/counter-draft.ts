// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { isCounterValue, normalizeCounterName } from '@bagel/kit/validation';

export type CounterDraft = {
  name: string;
  value: string;
};

export const NEW_COUNTER = '__new__';

export function blankCounter(): CounterDraft {
  return { name: '', value: '0' };
}

export function counterComplete(draft: CounterDraft): boolean {
  const name = normalizeCounterName(draft.name);
  if (!name || name.includes(':')) return false;
  return isCounterValue(draft.value);
}
