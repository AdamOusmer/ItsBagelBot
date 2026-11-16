// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface CounterReading {
  total: number;
  rate: number | null;
}

export interface CounterReadOptions {
  animateRate?: boolean;
}

function elapsedSeconds(now: number, at: number): number {
  return Math.max(0, now - at) / 1000;
}

export class StatsCounter {
  private origin: CounterReading;
  private at: number;
  private rateFrom: number | null;

  constructor(total: number, rate: number | null, now: number) {
    this.origin = { total, rate };
    this.at = now;
    this.rateFrom = rate;
  }

  update(rate: number | null, now: number, fallback: number | null = null): void {
    const next = rate ?? this.origin.rate ?? fallback;
    if (next === this.origin.rate) return;
    this.rateFrom = this.animatedRate(now);
    this.origin = { total: this.read(now).total, rate: next };
    this.at = now;
  }

  read(now: number, options: CounterReadOptions = {}): CounterReading {
    if (options.animateRate === false) this.rateFrom = this.origin.rate;
    const seconds = elapsedSeconds(now, this.at);
    return {
      total: this.origin.total + (this.origin.rate ?? 0) * seconds,
      rate: options.animateRate ? this.animatedRate(now) : this.origin.rate
    };
  }

  private animatedRate(now: number): number | null {
    const target = this.origin.rate;
    if (target === null || this.rateFrom === null) return target;
    const progress = Math.min(1, elapsedSeconds(now, this.at));
    return this.rateFrom + (target - this.rateFrom) * progress;
  }
}
