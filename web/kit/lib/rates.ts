// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Ingress (Ingress.Capacity) uses the same two windows; change them together.
export const RATE_NOW_SECONDS = 10;
export const RATE_AVG_SECONDS = 60;

const MIN_SPAN_MS = 1000;

export function perSecond(count: number | null | undefined, windowSeconds: number): number {
  if (!count || count < 0) return 0;
  return windowSeconds > 0 ? count / windowSeconds : 0;
}

export interface RateSample {
  messages: number;
  events: number;
  at: number;
}

export interface Rates {
  msg: number | null;
  event: number | null;
}

function sampleWindow<T extends { at: number }>(
  windowMs: number,
  backwards: (last: T, next: T) => boolean,
  calculate: (first: T, next: T, seconds: number) => Rates
): (next: T) => Rates {
  const samples: T[] = [];
  let rates: Rates = { msg: null, event: null };
  return (next) => {
    const last = samples.at(-1);
    if (last && backwards(last, next)) samples.length = 0;
    samples.push(next);
    while (samples.length > 1 && next.at - samples[1].at >= windowMs) samples.shift();
    const first = samples[0];
    const spanMs = next.at - first.at;
    if (spanMs < MIN_SPAN_MS) return rates;
    rates = calculate(first, next, spanMs / 1000);
    return rates;
  };
}

export function rateWindow(windowMs: number): (next: RateSample) => Rates {
  return sampleWindow<RateSample>(
    windowMs,
    (last, next) => next.messages < last.messages || next.events < last.events,
    (first, next, seconds) => ({
      msg: Math.max(next.messages - first.messages, 0) / seconds,
      event: Math.max(next.events - first.events, 0) / seconds
    })
  );
}

export function rateWindows(): (next: RateSample) => { now: Rates; avg: Rates } {
  const now = rateWindow(RATE_NOW_SECONDS * 1000);
  const avg = rateWindow(RATE_AVG_SECONDS * 1000);
  return (next) => ({ now: now(next), avg: avg(next) });
}

export interface ExactRateSample {
  messages: bigint;
  events: bigint;
  at: number;
}

export function exactRateWindow(windowMs: number): (next: ExactRateSample) => Rates {
  return sampleWindow<ExactRateSample>(
    windowMs,
    (last, next) => next.messages < last.messages || next.events < last.events,
    (first, next, seconds) => ({
      msg: Number(next.messages - first.messages) / seconds,
      event: Number(next.events - first.events) / seconds
    })
  );
}

export function exactRateWindows(): (next: ExactRateSample) => { now: Rates; avg: Rates } {
  const now = exactRateWindow(RATE_NOW_SECONDS * 1000);
  const avg = exactRateWindow(RATE_AVG_SECONDS * 1000);
  return (next) => ({ now: now(next), avg: avg(next) });
}
