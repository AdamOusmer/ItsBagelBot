// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// parseTimer validates and normalizes a posted timer JSON blob into a full
// TimerDef. Split out of timers/+page.server.ts (docs/specs/timer-conditions.md
// §7) because a +page.server.ts cannot export a helper a test can import.
//
// clampInt comes from the '@bagel/kit/validation' subpath, not the package
// barrel: the barrel eagerly loads i18n/messages.ts, which calls Vite's
// import.meta.glob and throws under plain `bun test` (no Vite transform).
// TimerDef stays a type-only import so it is erased before that barrel is
// ever touched.
import { clampInt } from '@bagel/kit/validation';
import type { TimerDef } from '@bagel/kit';

// Gate and stop ranges (docs/specs/timer-conditions.md §4, D12). sesame
// re-checks both defensively at arm time, same as it floors the interval.
const CONDITION_MIN = 0;
const CONDITION_MAX = 100;

// parseEndsAt accepts anything `Date` can parse and stores it as a UTC RFC
// 3339 instant; anything unparsable, including empty, drops to '' (never). A
// past instant is accepted as-is: the timer simply reads as Ended.
function parseEndsAt(raw: unknown): string {
  const s = String(raw ?? '').trim();
  if (!s) return '';
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? '' : d.toISOString();
}

// parseTimer validates and normalizes the posted timer JSON into a full
// TimerDef. Returns null on anything malformed. The interval is clamped to
// 60s-24h here; sesame floors it again defensively at arm time.
export function parseTimer(raw: string): TimerDef | null {
  let obj: Partial<TimerDef>;
  try {
    obj = JSON.parse(raw) as Partial<TimerDef>;
  } catch {
    return null;
  }
  const message = String(obj.message ?? '').trim();
  if (!message || message.length > 500) return null;

  return {
    id: String(obj.id ?? ''),
    message,
    intervalSeconds: clampInt(obj.intervalSeconds, 60, 86_400, 600),
    enabled: obj.enabled !== false,
    minChatLines: clampInt(obj.minChatLines, CONDITION_MIN, CONDITION_MAX, 0),
    maxFiresPerStream: clampInt(obj.maxFiresPerStream, CONDITION_MIN, CONDITION_MAX, 0),
    endsAt: parseEndsAt(obj.endsAt)
  };
}
