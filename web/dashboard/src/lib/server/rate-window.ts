// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const RATE_WINDOW_MS = 30_000;
const MIN_SPAN_MS = 1000;

export interface Sample {
  messages: number;
  events: number;
  at: number;
}

export interface Rates {
  msg: number | null;
  event: number | null;
}

function perSecond(current: number, previous: number, secs: number): number {
  return Math.max(current - previous, 0) / secs;
}

function wentBackwards(last: Sample | undefined, next: Sample): boolean {
  return !!last && (next.messages < last.messages || next.events < last.events);
}

export function rateWindow(windowMs = RATE_WINDOW_MS): (next: Sample) => Rates {
  const samples: Sample[] = [];
  let rates: Rates = { msg: null, event: null };
  return (next) => {
    if (wentBackwards(samples.at(-1), next)) samples.length = 0;
    samples.push(next);
    while (samples.length > 1 && next.at - samples[1].at >= windowMs) samples.shift();
    const first = samples[0];
    const spanMs = next.at - first.at;
    if (spanMs < MIN_SPAN_MS) return rates;
    rates = {
      msg: perSecond(next.messages, first.messages, spanMs / 1000),
      event: perSecond(next.events, first.events, spanMs / 1000)
    };
    return rates;
  };
}
