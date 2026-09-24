// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { subscribe } from './raf-loop';

const NEWTON_ITERATIONS = 8;

export type Ease = (t: number) => number;

export const linear: Ease = (t) => t;

export function bezier(x1: number, y1: number, x2: number, y2: number): Ease {
  const ax = 3 * x1 - 3 * x2 + 1;
  const bx = 3 * x2 - 6 * x1;
  const cx = 3 * x1;
  const ay = 3 * y1 - 3 * y2 + 1;
  const by = 3 * y2 - 6 * y1;
  const cy = 3 * y1;

  const sampleX = (t: number) => ((ax * t + bx) * t + cx) * t;
  const slopeX = (t: number) => (3 * ax * t + 2 * bx) * t + cx;
  const sampleY = (t: number) => ((ay * t + by) * t + cy) * t;

  return (x) => {
    let t = x;
    for (let i = 0; i < NEWTON_ITERATIONS; i++) {
      const slope = slopeX(t);
      if (slope === 0) break;
      t -= (sampleX(t) - x) / slope;
    }
    return sampleY(t);
  };
}

export interface TweenOptions {
  duration: number;
  delay?: number;
  ease?: Ease;
  from?: number;
  to?: number;
  onUpdate: (value: number) => void;
  onComplete?: () => void;
}

export function tween(options: TweenOptions): () => void {
  const {
    duration,
    delay = 0,
    ease = linear,
    from = 0,
    to = 1,
    onUpdate,
    onComplete,
  } = options;

  const ms = duration * 1000;
  const delayMs = delay * 1000;
  let start = 0;
  let unsubscribe: (() => void) | undefined;

  const tick = (now: number): boolean => {
    if (start === 0) start = now;
    const elapsed = now - start - delayMs;
    if (elapsed < 0) return true;

    const progress = ms <= 0 ? 1 : Math.min(elapsed / ms, 1);
    onUpdate(from + (to - from) * ease(progress));
    if (progress < 1) return true;

    unsubscribe?.();
    unsubscribe = undefined;
    onComplete?.();
    return false;
  };

  unsubscribe = subscribe(tick);

  return () => {
    unsubscribe?.();
    unsubscribe = undefined;
  };
}
