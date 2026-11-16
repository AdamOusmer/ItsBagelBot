// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface CounterReading {
  total: number;
  rate: number | null;
}

export interface CounterReadOptions {
  animateRate?: boolean;
}

export interface CounterUpdateOptions {
  total?: number;
  sampledAt?: number;
  snap?: boolean;
}

const RATE_TAU_SECONDS = 2;
const TOTAL_TAU_SECONDS = 4;
const MAX_EXTRAPOLATION_MS = 3000;

function elapsedSeconds(now: number, at: number): number {
  return Math.max(0, now - at) / 1000;
}

export class StatsCounter {
  private origin: CounterReading;
  private at: number;
  private rateFrom: number | null;
  private sampledAt: number;
  private truth: { total: number; at: number } | null = null;
  private shown: number;

  constructor(total: number, rate: number | null, now: number) {
    this.origin = { total, rate };
    this.at = now;
    this.rateFrom = rate;
    this.sampledAt = now;
    this.shown = total;
  }

  update(rate: number | null, now: number, fallback: number | null = null, options: CounterUpdateOptions = {}): void {
    const next = rate ?? this.origin.rate ?? fallback;
    const current = this.read(now);
    if (rate !== null) this.sampledAt = now;
    this.rateFrom = current.rate ?? next;
    this.origin = { total: current.total, rate: next };
    this.at = now;
    if (options.total === undefined) return;
    this.truth = { total: options.total, at: options.sampledAt ?? now };
    if (!options.snap) return;
    this.origin.total = this.serverTotal(now);
    this.rateFrom = next;
    this.shown = this.origin.total;
  }

  read(now: number, options: CounterReadOptions = {}): CounterReading {
    if (options.animateRate === false) this.useTargetRate(now);
    const seconds = elapsedSeconds(now, this.at);
    const decay = Math.exp(-seconds / RATE_TAU_SECONDS);
    const target = this.origin.rate ?? 0;
    const difference = (this.rateFrom ?? target) - target;
    this.shown = Math.max(this.shown, this.projectedTotal(now, difference));
    return {
      total: this.shown,
      rate: this.origin.rate === null ? null : target + difference * decay
    };
  }

  private useTargetRate(now: number): void {
    if (this.rateFrom === this.origin.rate) return;
    this.origin.total = this.read(now).total;
    this.at = now;
    this.rateFrom = this.origin.rate;
  }

  private serverTotal(now: number): number {
    if (!this.truth) return this.origin.total;
    const until = Math.min(now, this.truth.at + MAX_EXTRAPOLATION_MS);
    return this.truth.total + (this.origin.rate ?? 0) * elapsedSeconds(until, this.truth.at);
  }

  private projectedTotal(now: number, difference: number): number {
    const until = this.truth ? now : Math.min(now, this.sampledAt + MAX_EXTRAPOLATION_MS);
    const seconds = elapsedSeconds(until, this.at);
    const decay = Math.exp(-seconds / RATE_TAU_SECONDS);
    if (!this.truth) {
      return this.origin.total + (this.origin.rate ?? 0) * seconds
        + difference * RATE_TAU_SECONDS * (1 - decay);
    }
    const correction = Math.exp(-seconds / TOTAL_TAU_SECONDS);
    const error = this.origin.total - this.serverTotal(this.at);
    const gap = 1 / TOTAL_TAU_SECONDS - 1 / RATE_TAU_SECONDS;
    const response = gap === 0 ? seconds * decay : (decay - correction) / gap;
    return this.serverTotal(now) + error * correction + difference * response;
  }
}
