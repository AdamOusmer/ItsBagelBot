// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { exactDisplay } from './stats-values';
import type { PublicStats } from './server/public-stats';

export type Frame = { messages: number; events: number; msgRate: number; eventRate: number };

const TAU_MS = 200;
const RATE_TAU_MS = 300;
const MAX_DT_MS = 250;
const RESUME_GAP_MS = 1000;
const MAX_PROJECT_S = 10;
const MIN_CRAWL = 0.25;

export function seedFrame(seed: PublicStats): Frame {
  return {
    messages: exactDisplay(Number(seed.messages_total)),
    events: exactDisplay(Number(seed.events_total)),
    msgRate: seed.msg_rate_now ?? seed.msg_rate ?? 0,
    eventRate: seed.event_rate_now ?? seed.event_rate ?? 0
  };
}

export function targetFrame(live: PublicStats, snapAt: number, now: number): Frame {
  const secs = Math.min(Math.max(0, now - snapAt) / 1000, MAX_PROJECT_S);
  const msgAvg = live.msg_rate ?? 0;
  const eventAvg = live.event_rate ?? 0;
  return {
    messages: exactDisplay(Number(live.messages_total) + msgAvg * secs),
    events: exactDisplay(Number(live.events_total) + eventAvg * secs),
    msgRate: live.msg_rate_now ?? msgAvg,
    eventRate: live.event_rate_now ?? eventAvg
  };
}

function lerp(from: number, to: number, eased: number): number {
  return from + (to - from) * eased;
}

function advance(cur: number, target: number, rate: number, tick: { dt: number; k: number }): number {
  return exactDisplay(Math.max(lerp(cur, target, tick.k), cur + rate * (tick.dt / 1000) * MIN_CRAWL));
}

export const resumedAfterGap = (now: number, lastFrame: number): boolean => now - lastFrame > RESUME_GAP_MS;

export const frameDelta = (now: number, lastFrame: number): number => Math.min(now - lastFrame, MAX_DT_MS);

export function stepFrame(display: Frame, target: Frame, dt: number): Frame {
  const k = 1 - Math.exp(-dt / TAU_MS);
  const rateK = 1 - Math.exp(-dt / RATE_TAU_MS);
  const tick = { dt, k };
  return {
    messages: advance(display.messages, target.messages, target.msgRate, tick),
    events: advance(display.events, target.events, target.eventRate, tick),
    msgRate: lerp(display.msgRate, target.msgRate, rateK),
    eventRate: lerp(display.eventRate, target.eventRate, rateK)
  };
}

export function rewindTotals(display: Frame, prev: PublicStats, next: PublicStats): Frame {
  return {
    ...display,
    messages: BigInt(next.messages_total) < BigInt(prev.messages_total) ? exactDisplay(Number(next.messages_total)) : display.messages,
    events: BigInt(next.events_total) < BigInt(prev.events_total) ? exactDisplay(Number(next.events_total)) : display.events
  };
}
