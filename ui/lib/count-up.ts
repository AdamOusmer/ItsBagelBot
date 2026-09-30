// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';
import { subscribe } from './raf-loop';

type Parsed = { target: number; suffix: string; grouped: boolean };

function parseCount(raw: string): Parsed | null {
  const m = raw.match(/^[\d,]+/);
  if (!m) return null;
  const digits = m[0];
  const target = Number(digits.replace(/,/g, ''));
  if (!Number.isFinite(target) || target <= 0) return null;
  return { target, suffix: raw.slice(digits.length), grouped: digits.includes(',') };
}

function format(n: number, grouped: boolean): string {
  return grouped ? Math.round(n).toLocaleString() : String(Math.round(n));
}

function easeOutQuart(p: number): number {
  return 1 - Math.pow(1 - p, 4);
}

type CountUpOptions = { durationMs?: number; value?: string };

export function countUp(
  node: HTMLElement,
  opts?: CountUpOptions,
): { update(next?: CountUpOptions): void; destroy(): void } | undefined {
  if (typeof window === 'undefined' || prefersReducedMotion()) return;

  const parsed = parseCount((node.textContent ?? '').trim());
  if (!parsed) return;

  const duration = opts?.durationMs ?? 900;
  const t0 = performance.now();

  const stop = subscribe((t) => {
    const p = Math.min(1, (t - t0) / duration);
    const eased = easeOutQuart(p);
    node.textContent = format(parsed.target * eased, parsed.grouped) + parsed.suffix;
    if (p < 1) return;
    stop();
    return false;
  });

  return {
    update(next) {
      if (next?.value === undefined) return;
      stop();
      node.textContent = next.value;
    },
    destroy: stop,
  };
}
