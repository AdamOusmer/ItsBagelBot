// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Counter writes arrive in batches. Average their deltas over a full window
// rather than treating each flush (and the quiet samples between it) as a new
// speed. Correct the displayed total through a small change in speed.
const RATE_WINDOW_MS = 30_000;
const SPEED_TAU_S = 5;
const CORRECTION_S = 60;
const MAX_CORRECTION = 0.1;
const COAST_MS = 10_000;
const STOP_MS = 30_000;
const MAX_DT_MS = 250;

interface Sample {
  total: bigint;
  at: number;
}

export class StatsCounter {
  value: number;
  rate: number;
  private samples: Sample[];
  private targetRate: number;
  private observedGrowth = false;

  constructor(total: number | string, rate: number | null, now: number) {
    this.value = Math.min(Number(total), Number.MAX_SAFE_INTEGER);
    this.rate = this.targetRate = Math.max(0, rate ?? 0);
    this.samples = [{ total: BigInt(total), at: now }];
  }

  sample(rawTotal: number | string, rate: number | null, now: number): void {
    const last = this.samples[this.samples.length - 1];
    const total = BigInt(rawTotal);
    if (total < last.total) {
      this.reset(total, rate, now);
      return;
    }
    this.observedGrowth ||= total > last.total;
    this.recordSample(total, now);
    this.updateRate(rate, now);
  }

  private reset(total: bigint, rate: number | null, now: number): void {
    // A real reset starts a new history; ordinary estimation error never
    // makes the displayed digits run backwards.
    this.value = Math.min(Number(total), Number.MAX_SAFE_INTEGER);
    this.rate = this.targetRate = Math.max(0, rate ?? 0);
    this.observedGrowth = false;
    this.samples = [{ total, at: now }];
  }

  private recordSample(total: bigint, now: number): void {
    const last = this.samples[this.samples.length - 1];
    if (now === last.at) this.samples[this.samples.length - 1] = { total, at: now };
    else this.samples.push({ total, at: now });

    // Keep the sample just before the cutoff so a batch at the boundary is
    // counted over its full elapsed time.
    const cutoff = now - RATE_WINDOW_MS;
    while (this.samples.length > 1 && this.samples[1].at <= cutoff) this.samples.shift();
  }

  private updateRate(rate: number | null, now: number): void {
    const first = this.samples[0];
    const elapsed = now - first.at;
    if (elapsed <= 0) {
      this.bootstrapRate(rate);
      return;
    }
    if (!this.observedGrowth) {
      if (elapsed < RATE_WINDOW_MS) {
        this.bootstrapRate(rate);
        return;
      }
    }
    const last = this.samples[this.samples.length - 1];
    this.targetRate = Math.max(0, Number(last.total - first.total)) / (elapsed / 1000);
  }

  private bootstrapRate(rate: number | null): void {
    // Bootstrap from the server until we have a useful delta of our own.
    if (this.targetRate === 0) this.targetRate = Math.max(0, rate ?? 0);
  }

  advance(now: number, elapsedMs: number): void {
    const last = this.samples[this.samples.length - 1];
    const age = Math.max(0, now - last.at);
    if (age >= STOP_MS) {
      this.rate = 0;
      return;
    }

    const secs = Math.max(0, Math.min(elapsedMs, MAX_DT_MS)) / 1000;
    const target = Number(last.total) + this.targetRate * Math.min(age, COAST_MS) / 1000;
    const limit = this.targetRate * MAX_CORRECTION;
    // Above the animation limit the page prints the exact wire total. Keep
    // measuring its bigint deltas, without correcting toward an unreachable
    // floating-point position.
    const correction = last.total > BigInt(Number.MAX_SAFE_INTEGER) ? 0
      : Math.max(-limit, Math.min(limit, (target - this.value) / CORRECTION_S));
    const speed = age <= COAST_MS ? this.targetRate + correction : 0;
    const decay = Math.exp(-secs / SPEED_TAU_S);
    // Integrate the same smoothly changing speed printed beneath the total.
    this.value = Math.min(Number.MAX_SAFE_INTEGER,
      this.value + speed * secs + (this.rate - speed) * SPEED_TAU_S * (1 - decay));
    this.rate = speed + (this.rate - speed) * decay;
  }

  snap(now: number): void {
    const last = this.samples[this.samples.length - 1];
    const age = Math.max(0, now - last.at);
    this.value = Math.min(Number.MAX_SAFE_INTEGER,
      Number(last.total) + this.targetRate * Math.min(age, COAST_MS) / 1000);
    this.rate = age <= COAST_MS ? this.targetRate : 0;
  }
}
