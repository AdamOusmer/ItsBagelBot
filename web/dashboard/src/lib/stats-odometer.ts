// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Play one snapshot interval behind the source. Most frames can then interpolate
// confirmed totals instead of repeatedly easing toward a moving prediction.
export const COUNTER_BUFFER_MS = 2000;
const STALE_MS = 10_000;
const MAX_SAMPLES = 16;

interface Sample {
  total: number;
  rate: number | null;
  at: number;
}

export interface CounterReading {
  total: number;
  rate: number | null;
}

function interpolate(first: Sample, next: Sample, at: number): CounterReading {
  const speed = (next.total - first.total) * 1000 / (next.at - first.at);
  const measured = first.rate ?? next.rate;
  return {
    total: first.total + speed * (at - first.at) / 1000,
    rate: measured === null && first.total === next.total ? null : speed
  };
}

function project(last: Sample, now: number): CounterReading {
  // A late frame or the 5s fallback poll can exhaust the buffer. Continue
  // at the last measured rate, but stop after ten seconds without truth.
  const limit = last.at + STALE_MS - COUNTER_BUFFER_MS;
  const elapsed = Math.max(0, Math.min(now - COUNTER_BUFFER_MS, limit) - last.at);
  return {
    total: last.total + (last.rate ?? 0) * elapsed / 1000,
    rate: now < last.at + STALE_MS ? last.rate : 0
  };
}

export class StatsCounter {
  private samples: Sample[];
  private printed = 0;

  constructor(total: number, rate: number | null, at: number, minimum = 0) {
    // Keep a resumed view (including a reduced-motion toggle) from moving
    // backward just to refill the buffer. A source reset may still lower it.
    this.printed = Math.min(total, minimum);
    // Supply the initial interval from the known rate, until two real samples
    // exist. The counter starts running immediately, with the same short delay
    // it will have during normal playback. There is no entrance tween.
    this.samples = [
      { total: Math.max(0, total - (rate ?? 0) * COUNTER_BUFFER_MS / 1000), rate, at: at - COUNTER_BUFFER_MS },
      { total, rate, at }
    ];
  }

  push(total: number, rate: number | null, at: number): void {
    const last = this.samples[this.samples.length - 1];
    // Cached snapshots and out-of-order responses cannot change the trajectory.
    if (at <= last.at) return;
    if (total < last.total) {
      // An actual source reset is allowed to lower the displayed total.
      this.samples = [{ total, rate, at }];
      this.printed = total;
      return;
    }
    this.samples.push({ total, rate, at });
    if (this.samples.length > MAX_SAMPLES) this.samples.shift();
  }

  read(now: number): CounterReading {
    const at = now - COUNTER_BUFFER_MS;
    this.trim(at);
    const reading = this.position(at, now);

    // When an unusually late sample corrects a prediction downward, hold until
    // confirmed growth catches up. The rate readout must describe that hold too.
    if (reading.total < this.printed) return { total: this.printed, rate: 0 };
    this.printed = reading.total;
    return reading;
  }

  private trim(at: number): void {
    while (this.samples.length > 2 && this.samples[1].at <= at) this.samples.shift();
  }

  private position(at: number, now: number): CounterReading {
    const last = this.samples[this.samples.length - 1];
    if (at >= last.at) return project(last, now);
    const first = this.samples[0];
    if (at < first.at) return { total: first.total, rate: 0 };
    return interpolate(first, this.samples[1], at);
  }
}
