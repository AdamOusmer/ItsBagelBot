// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { liveTotals } from './live-counters';
import { exactRateWindow, RATE_AVG_SECONDS } from '@bagel/kit/rates';

// process.env, not $app/env/private: it only exposes variables declared in src/env.ts.
const DEMO = dev && process.env.DEMO === '1';

const BOT_SCOPE_USER = '0';
const COUNTER_MESSAGES = 'messages_processed';
const COUNTER_EVENTS = 'events_processed';
const COUNTERS = [COUNTER_MESSAGES, COUNTER_EVENTS] as const;

// Viewer-driven sampling left each pod's windows holding one stale sample after idle; the timer keeps them full.
const SAMPLE_MS = 1000;
const PUBLIC_RATE_NOW_SECONDS = 10;
const RATE_MIN_SPAN_MS = 5000;
const WARMUP_CAP_MS = 8000;

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

const sampleNow = exactRateWindow(PUBLIC_RATE_NOW_SECONDS * 1000, RATE_MIN_SPAN_MS);
const sampleAverage = exactRateWindow(RATE_AVG_SECONDS * 1000, RATE_MIN_SPAN_MS);

let latest: PublicStats | null = null;
let inflight: Promise<void> | null = null;
let timer: ReturnType<typeof setInterval> | null = null;
let startedAt: number | null = null;
let warmed = DEMO;

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
  const snapshot = { messages: BigInt(messages), events: BigInt(events), at: sampledAt };
  const now = sampleNow(snapshot);
  const average = sampleAverage(snapshot);
  return {
    messages_total: messages,
    events_total: events,
    sampled_at: sampledAt,
    msg_rate: average.msg,
    event_rate: average.event,
    msg_rate_now: now.msg ?? average.msg,
    event_rate_now: now.event ?? average.event,
    degraded: false
  };
}

function sample(): Promise<void> {
  inflight ??= loadStats()
    .then((stats) => {
      latest = stats;
      if (!stats.degraded && stats.msg_rate_now !== null) warmed = true;
    })
    .catch(() => {
      latest = degradedStats();
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

export function startPublicStats(): void {
  if (DEMO) return;
  if (timer) return;
  startedAt = Date.now();
  void sample();
  timer = setInterval(() => void sample(), SAMPLE_MS);
  timer.unref?.();
}

export function publicStatsWarm(): boolean {
  if (warmed) return true;
  if (startedAt === null) return false;
  warmed = Date.now() - startedAt >= WARMUP_CAP_MS;
  return warmed;
}

export async function publicStats(): Promise<PublicStats> {
  if (DEMO) return (await import('./demo-data')).demoStats(Date.now());
  startPublicStats();
  if (!latest || latest.degraded) await sample();
  return latest ?? degradedStats();
}
