// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import { dev } from '$app/environment';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { fabric, SUB } from './services';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
const DEMO = dev && process.env.DEMO === '1';

const CACHE_KEY = 'public-stats:global';

const BOT_SCOPE_USER = '0';
const COUNTER_MESSAGES = 'messages_processed';
const COUNTER_EVENTS = 'events_processed';

const RPC_TIMEOUT_MS = 4000;

const MIN_SAMPLE_MS = 1000;

export interface PublicStats {
  messages_total: number;
  events_total: number;
  msg_rate: number | null;
  event_rate: number | null;
  degraded: boolean;
}

interface CounterWire {
  name?: string;
  scope?: string;
  value?: number;
}

interface LoyaltyReplyWire {
  counter?: CounterWire;
  found?: boolean;
  error?: string;
}

async function counterValue(name: string): Promise<number | null> {
  try {
    const reply = await rpc<LoyaltyReplyWire>(
      `${SUB.loyalty}.counter.get`,
      { user_id: BOT_SCOPE_USER, name },
      RPC_TIMEOUT_MS
    );
    const raw = reply.counter?.value;
    return Number.isFinite(raw) ? Number(raw) : 0;
  } catch {
    return null;
  }
}

interface Sample {
  messages: number;
  events: number;
  at: number;
}

let prev: Sample | null = null;
let lastRates: { msg: number | null; event: number | null } = { msg: null, event: null };

function perSecond(current: number, previous: number, secs: number): number {
  return Math.max(current - previous, 0) / secs;
}

function sampleRates(messages: number, events: number, now: number): { msg: number | null; event: number | null } {
  const before = prev;
  if (!before) {
    prev = { messages, events, at: now };
    return lastRates;
  }
  const elapsedMs = now - before.at;
  if (elapsedMs < MIN_SAMPLE_MS) return lastRates;

  const secs = elapsedMs / 1000;
  prev = { messages, events, at: now };
  lastRates = {
    msg: perSecond(messages, before.messages, secs),
    event: perSecond(events, before.events, secs)
  };
  return lastRates;
}

function degradedStats(): PublicStats {
  return { messages_total: 0, events_total: 0, msg_rate: null, event_rate: null, degraded: true };
}

async function loadStats(): Promise<PublicStats> {
  const [messages, events] = await Promise.all([counterValue(COUNTER_MESSAGES), counterValue(COUNTER_EVENTS)]);
  if (messages === null || events === null) return degradedStats();

  const rates = sampleRates(messages, events, Date.now());
  return {
    messages_total: messages,
    events_total: events,
    msg_rate: rates.msg,
    event_rate: rates.event,
    degraded: false
  };
}

export async function publicStats(): Promise<PublicStats> {
  if (DEMO) return (await import('./demo-data')).demoStats(Date.now());
  try {
    return await fabric.readKey(CACHE_KEY, POLICY.live, loadStats);
  } catch {
    return degradedStats();
  }
}
