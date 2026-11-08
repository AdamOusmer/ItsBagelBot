// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface CounterReading {
  total: number;
  rate: number | null;
}

// Each page load establishes the total. Subsequent fetches only change its
// rate: batched totals, missing data, and pauses in polling must never slow the
// clock down or reset its accumulated count. A reload reconciles the total.
export class StatsCounter {
  private origin: CounterReading;
  private at: number;

  constructor(total: number, rate: number | null, now: number) {
    this.origin = { total, rate };
    this.at = now;
  }

  update(rate: number | null, now: number, fallback: number | null = null): void {
    const next = rate ?? this.origin.rate ?? fallback;
    if (next === this.origin.rate) return;
    // Finish the elapsed time at the OLD rate before starting the new one.
    this.origin = { total: this.read(now).total, rate: next };
    this.at = now;
  }

  read(now: number): CounterReading {
    const seconds = Math.max(0, now - this.at) / 1000;
    return {
      total: this.origin.total + (this.origin.rate ?? 0) * seconds,
      rate: this.origin.rate
    };
  }
}
