// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { isCounterValue, normalizeCounterName } from '@bagel/kit/validation';

export type CounterDraft = {
  name: string;
  value: string;
};

export const NEW_COUNTER = '';
export const COUNTERS_DATA = 'admin:counters';

export function blankCounter(): CounterDraft {
  return { name: '', value: '0' };
}

export function counterName(raw: unknown): string | null {
  const name = normalizeCounterName(raw);
  return name.length > 0 && !name.includes(':') ? name : null;
}

export function counterComplete(draft: CounterDraft): boolean {
  return counterName(draft.name) !== null && isCounterValue(draft.value);
}
