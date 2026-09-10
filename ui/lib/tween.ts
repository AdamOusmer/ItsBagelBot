// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// A one-value tween on the shared rAF scheduler, plus the cubic-bezier solver
// it eases with.
//
// Why this exists rather than a dependency: the marketing nav's open/close
// choreography was written against the `motion` package (three concurrent
// `animate(0, 1, { ease: cubicBezier(…) })` calls), and `motion` is a 30 KB
// dependency that the site already carries but @bagel/ui must not -- the
// library declares exactly one runtime dependency, `lenis`, and the argument
// for that one is that a smooth-scroll implementation is not something to
// hand-write. A driven number on a curve is.
//
// Why not a CSS transition, which needs no code at all: the values being
// animated are an SVG `d` attribute (a quadratic curve whose control point
// moves with progress) and a per-item transform whose offset is computed from
// the item's index. Neither is a property CSS can interpolate.
//
// Measured 2026-09-09: bezier + tween together are 341 B gzip, against ~30 KB
// for the smallest `motion` entry that exports `animate` and `cubicBezier`.

import { subscribe } from './raf-loop';

export type Ease = (t: number) => number;

/** Linear, and the default: a caller that wants a curve passes one. */
export const linear: Ease = (t) => t;

/**
 * A CSS `cubic-bezier(x1, y1, x2, y2)` as a function of progress.
 *
 * Newton-Raphson on the x polynomial, which is what every browser engine and
 * every animation library does here, for the same reason: the curve is
 * parameterised by t, but an animation needs y at a given X (elapsed/duration),
 * and there is no closed form. Eight iterations rather than a sampling table
 * because the table is the memory-vs-accuracy trade a 60fps tween does not
 * need to make -- at eight iterations the error is below a thousandth of a
 * pixel on a 1000px travel, and the whole solve is ~30 flops per frame.
 */
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
    for (let i = 0; i < 8; i++) {
      const slope = slopeX(t);
      // A flat segment: Newton would divide by ~0 and throw t to infinity.
      // Stopping here is correct, not a fallback -- x is already as close as
      // this curve gets.
      if (slope === 0) break;
      t -= (sampleX(t) - x) / slope;
    }
    return sampleY(t);
  };
}

export interface TweenOptions {
  /** Seconds, matching the `motion` call signature this replaces. */
  duration: number;
  /** Seconds before the first update. The stagger's only knob. */
  delay?: number;
  ease?: Ease;
  from?: number;
  to?: number;
  onUpdate: (value: number) => void;
  onComplete?: () => void;
}

/**
 * Run one tween. Returns `stop()`, which is silent: it does NOT fire
 * `onComplete`, because every caller here stops a tween in order to start its
 * opposite, and a completion callback firing on the way out would paint the
 * end state of the animation being abandoned.
 */
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
  // Assigned before the first tick can run: `subscribe` schedules a frame, it
  // does not call back synchronously.
  let unsubscribe: (() => void) | undefined;

  const tick = (now: number): boolean => {
    // The scheduler hands over the frame timestamp rather than a start time,
    // so the first tick is where the clock starts. This also makes a delayed
    // tween cost nothing until it runs.
    if (start === 0) start = now;
    const elapsed = now - start - delayMs;
    if (elapsed < 0) return true;

    const progress = ms <= 0 ? 1 : Math.min(elapsed / ms, 1);
    onUpdate(from + (to - from) * ease(progress));
    if (progress < 1) return true;

    // Leaving the scheduler entirely, not just settling: a settled subscriber
    // stays in the set and a page-wide `wake()` -- which the cursor and the
    // mote field both call -- would re-run a finished tween's last frame.
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
