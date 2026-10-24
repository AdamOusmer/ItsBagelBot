// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { TimerDef } from '@bagel/kit';
import { urlFetchNames, URLFETCH_TOKEN_CAP } from '@bagel/kit/engine/fetch-validate';

export type TimerError = 'message' | 'interval' | 'urlfetch';

const MIN_INTERVAL_SECONDS = 60;
const MAX_INTERVAL_SECONDS = 86_400;

const intervalInRange = (seconds: number) =>
  Number.isFinite(seconds) && seconds >= MIN_INTERVAL_SECONDS && seconds <= MAX_INTERVAL_SECONDS;

export function firstTimerError(draft: TimerDef): TimerError | null {
  if (!draft.message.trim()) return 'message';
  if (!intervalInRange(draft.intervalSeconds)) return 'interval';
  if (urlFetchNames(draft.message).length > URLFETCH_TOKEN_CAP) return 'urlfetch';
  return null;
}
