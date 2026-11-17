// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { liveTotals } from './live-counters';
import { exactRateWindows } from '@bagel/kit/rates';

// process.env, not $app/env/private: it only exposes variables declared in src/env.ts.
const DEMO = dev && process.env.DEMO === '1';

const BOT_SCOPE_USER = '0';
const COUNTER_MESSAGES = 'messages_processed';
const COUNTER_EVENTS = 'events_processed';
const COUNTERS = [COUNTER_MESSAGES, COUNTER_EVENTS] as const;

// Viewer-driven sampling left each pod's windows holding one stale sample after idle; the timer keeps them full.
const SAMPLE_MS = 1000;

export interface PublicStats {
  messages_total: string;
  events_total: string;
  /** Source sampling time; absent on snapshots from an older server. */
  sampled_at?: number | null;
  msg_rate: number | null;
  event_rate: number | null;
  msg_rate_now: number | null;
  event_rate_now: number | null;
  degraded: boolean;
}

const sampleRates = exactRateWindows();

let latest: PublicStats | null = null;
let inflight: Promise<void> | null = null;
let timer: ReturnType<typeof setInterval> | null = null;

function degradedStats(): PublicStats {
  return {
    messages_total: '0',
    events_total: '0',
    sampled_at: null,
    msg_rate: null,
    event_rate: null,
    msg_rate_now: null,
    event_rate_now: null,
    degraded: true
  };
}

async function loadStats(): Promise<PublicStats> {
  const totals = await liveTotals(BOT_SCOPE_USER, COUNTERS);
  if (!totals) return degradedStats();

  const messages = totals[COUNTER_MESSAGES];
  const events = totals[COUNTER_EVENTS];
  const sampledAt = Date.now();
  const rates = sampleRates({ messages: BigInt(messages), events: BigInt(events), at: sampledAt });
  return {
    messages_total: messages,
    events_total: events,
    sampled_at: sampledAt,
    msg_rate: rates.avg.msg,
    event_rate: rates.avg.event,
    msg_rate_now: rates.now.msg,
    event_rate_now: rates.now.event,
    degraded: false
  };
}

function sample(): Promise<void> {
  inflight ??= loadStats()
    .then((stats) => {
      latest = stats;
    })
    .catch(() => {
      latest = degradedStats();
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

function startSampler(): void {
  if (timer) return;
  timer = setInterval(() => void sample(), SAMPLE_MS);
  timer.unref?.();
}

export async function publicStats(): Promise<PublicStats> {
  if (DEMO) return (await import('./demo-data')).demoStats(Date.now());
  startSampler();
  if (!latest) await sample();
  return latest ?? degradedStats();
}
