// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { masterClient } from '@bagel/kit/server/valkey-master';
import { CircuitBreaker, withTimeout } from '@bagel/kit/server/resilience';
import { liveTotals } from './live-counters';
import { degradedStreamCounters, type StreamCounters } from '$lib/overview-live';
import { parseCounterValue } from '@bagel/kit/validation';

const SETTINGS_PREFIX = 'settings:';

// Must match internal/projection/valkey.go's streamCtr*Field names.
const FIELD_MESSAGES = 'streamctr:messages';
const FIELD_ANSWERED = 'streamctr:answered';
const FIELD_MOD_ACTIONS = 'streamctr:mod_actions';

const COUNTER_MESSAGES = 'messages_processed';
const COUNTER_ANSWERED = 'commands_answered';
const COUNTER_MOD_ACTIONS = 'mod_actions';

const OP_TIMEOUT_MS = 200;

const breaker = new CircuitBreaker({ name: 'valkey-stream-counters', failureThreshold: 3, resetMs: 5_000 });

interface Baseline {
  known: boolean;
  messages: string;
  answered: string;
  modActions: string;
}

const MISS_BASELINE: Baseline = { known: false, messages: '0', answered: '0', modActions: '0' };

async function readBaseline(uid: string): Promise<Baseline> {
  const c = masterClient();
  if (!c) return MISS_BASELINE;
  try {
    const [messages, answered, modActions] = await breaker.run(() =>
      withTimeout(
        c.hmget(SETTINGS_PREFIX + uid, FIELD_MESSAGES, FIELD_ANSWERED, FIELD_MOD_ACTIONS),
        OP_TIMEOUT_MS,
        'valkey-stream-counters'
      )
    );
    if (messages === null) return MISS_BASELINE;
    const msg = parseCounterValue(messages);
    const ans = parseCounterValue(answered ?? '0');
    const mods = parseCounterValue(modActions ?? '0');
    if (msg === null || ans === null || mods === null) return MISS_BASELINE;
    return {
      known: true,
      messages: msg,
      answered: ans,
      modActions: mods
    };
  } catch {
    return MISS_BASELINE;
  }
}

function clampDelta(current: string, baseline: string): string {
  const delta = BigInt(current) - BigInt(baseline);
  return delta > 0n ? delta.toString() : '0';
}

const COUNTERS = [COUNTER_MESSAGES, COUNTER_ANSWERED, COUNTER_MOD_ACTIONS] as const;

export async function streamCounters(uid: string): Promise<StreamCounters> {
  const [baseline, totals] = await Promise.all([readBaseline(uid), liveTotals(uid, COUNTERS)]);
  if (!baseline.known || !totals) return degradedStreamCounters();

  return {
    messages: clampDelta(totals[COUNTER_MESSAGES], baseline.messages),
    answered: clampDelta(totals[COUNTER_ANSWERED], baseline.answered),
    modActions: clampDelta(totals[COUNTER_MOD_ACTIONS], baseline.modActions),
    ok: true
  };
}
