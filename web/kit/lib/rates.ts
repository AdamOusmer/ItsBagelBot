// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Ingress (Ingress.Capacity) uses the same two windows; change them together.
export const RATE_NOW_SECONDS = 10;
export const RATE_AVG_SECONDS = 60;

const MIN_SPAN_MS = 1000;

export function perSecond(count: number | null | undefined, windowSeconds: number): number {
  if (!count || count <= 0 || windowSeconds <= 0) return 0;
  return count / windowSeconds;
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

function wentBackwards(last: RateSample | undefined, next: RateSample): boolean {
  return !!last && (next.messages < last.messages || next.events < last.events);
}

export function rateWindow(windowMs: number): (next: RateSample) => Rates {
  const samples: RateSample[] = [];
  let rates: Rates = { msg: null, event: null };
  return (next) => {
    if (wentBackwards(samples.at(-1), next)) samples.length = 0;
    samples.push(next);
    while (samples.length > 1 && next.at - samples[1].at >= windowMs) samples.shift();
    const first = samples[0];
    const spanMs = next.at - first.at;
    if (spanMs < MIN_SPAN_MS) return rates;
    const secs = spanMs / 1000;
    rates = {
      msg: Math.max(next.messages - first.messages, 0) / secs,
      event: Math.max(next.events - first.events, 0) / secs
    };
    return rates;
  };
}

export function rateWindows(): (next: RateSample) => { now: Rates; avg: Rates } {
  const now = rateWindow(RATE_NOW_SECONDS * 1000);
  const avg = rateWindow(RATE_AVG_SECONDS * 1000);
  return (next) => ({ now: now(next), avg: avg(next) });
}
