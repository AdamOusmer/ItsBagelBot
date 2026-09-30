// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { clampInt } from '@bagel/kit/validation';
import { urlFetchNames, URLFETCH_TOKEN_CAP } from '@bagel/kit/engine/fetch-validate';
import { DEFAULT_CHAT_WINDOW_MINUTES } from '@bagel/kit/timers';
import type { TimerDef } from '@bagel/kit';

const CONDITION_MIN = 0;
const CONDITION_MAX = 100;
const WINDOW_MIN = 1;
const WINDOW_MAX = 60;

function parseEndsAt(raw: unknown): string {
  const s = String(raw ?? '').trim();
  if (!s) return '';
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? '' : d.toISOString();
}

export function parseTimer(raw: string): TimerDef | null {
  let obj: Partial<TimerDef>;
  try {
    obj = JSON.parse(raw) as Partial<TimerDef>;
  } catch {
    return null;
  }
  const message = String(obj.message ?? '').trim();
  if (!message || message.length > 500) return null;
  if (urlFetchNames(message).length > URLFETCH_TOKEN_CAP) return null;

  return {
    id: String(obj.id ?? ''),
    message,
    intervalSeconds: clampInt(obj.intervalSeconds, 60, 86_400, 600),
    enabled: obj.enabled !== false,
    minChatLines: clampInt(obj.minChatLines, CONDITION_MIN, CONDITION_MAX, 0),
    chatWindowMinutes: clampInt(obj.chatWindowMinutes, WINDOW_MIN, WINDOW_MAX, DEFAULT_CHAT_WINDOW_MINUTES),
    allowOffline: obj.allowOffline === true,
    maxFiresPerStream: clampInt(obj.maxFiresPerStream, CONDITION_MIN, CONDITION_MAX, 0),
    endsAt: parseEndsAt(obj.endsAt)
  };
}
