// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Google-style magnetic hover: while the pointer is over the element it eases
// toward the cursor by `strength` of the offset from centre, capped at `max`
// px, and releases back to rest on leave.
//
// Framework-free, like everything else in this directory: it takes a node and
// returns a teardown. The Svelte spelling is `use:magnetic`
// (../svelte/actions.ts) and the static spelling is `data-magnetic` on the
// element plus one `observeMagnetic(document)` call per page, which is how the
// marketing site wires every other engine in here.
//
// It was web/kit/lib/actions.ts's, console-only, even though it already ran on
// this package's shared scheduler and this package's motion queries. Moving it
// is what lets the marketing site's buttons have the same hover as the
// console's; kit re-exports the action so its ~40 call sites did not change.

import { hasFinePointer, prefersReducedMotion } from './motion-query';
import { subscribe, wake, type Tick } from './raf-loop';

export type MagneticOptions = { strength?: number; max?: number };

/**
 * Interpolation factor per frame: the element closes 20% of the remaining gap
 * to the pointer each tick. Tuned as a pair with the settle threshold below.
 * At 0.2 / 0.1px the follow reads as weighted and comes to rest about half a
 * second after the pointer stops. 0.35 tracks the cursor tightly enough that
 * the magnetism stops reading as magnetism; 0.1 leaves the element visibly
 * still drifting after the pointer has left the button, which testers read as
 * lag rather than as easing.
 */
const EASE = 0.2;

/**
 * Settle threshold in CSS px. Asymptotic easing never reaches its target
 * exactly, so without a threshold this tick would request a frame forever and
 * the shared scheduler could never suspend — the whole reason it has a settle
 * protocol. 0.1 and not 0.5 because the transform below is written with
 * `toFixed(2)` and a 2x display does render sub-pixel offsets.
 */
const SETTLE_PX = 0.1;

const NOOP = () => {};

/**
 * Wire one element. Returns a teardown.
 *
 * Ticked by this package's one shared scheduler rather than its own
 * requestAnimationFrame: a toolbar of magnetic buttons used to register one
 * loop per button, each reading `getBoundingClientRect` in its own callback
 * inside the same frame.
 *
 * The pointer gate is `(hover: hover) and (pointer: fine)` and not
 * `(pointer: fine)` alone: a stylus reports a fine pointer and cannot hover,
 * so it used to get a magnetic offset it could never see resting.
 */
export function mountMagnetic(node: HTMLElement, opts?: MagneticOptions): () => void {
  if (!hasFinePointer() || prefersReducedMotion()) return NOOP;

  const strength = opts?.strength ?? 0.3;
  const max = opts?.max ?? 14;
  let tx = 0,
    ty = 0,
    cx = 0,
    cy = 0;

  node.style.transition = 'transform 0.3s cubic-bezier(0.16,1,0.3,1)';
  node.style.willChange = 'transform';

  const cap = (v: number) => Math.max(-max, Math.min(max, v));

  const tick: Tick = () => {
    cx += (tx - cx) * EASE;
    cy += (ty - cy) * EASE;
    node.style.transform = `translate(${cx.toFixed(2)}px, ${cy.toFixed(2)}px)`;
    return Math.abs(tx - cx) > SETTLE_PX || Math.abs(ty - cy) > SETTLE_PX;
  };
  const unsubscribe = subscribe(tick);

  const move = (e: PointerEvent) => {
    const r = node.getBoundingClientRect();
    tx = cap((e.clientX - (r.left + r.width / 2)) * strength);
    ty = cap((e.clientY - (r.top + r.height / 2)) * strength);
    node.style.transition = 'none';
    wake(tick);
  };
  const leave = () => {
    tx = 0;
    ty = 0;
    node.style.transition = 'transform 0.4s cubic-bezier(0.16,1,0.3,1)';
    wake(tick);
  };

  node.addEventListener('pointermove', move);
  node.addEventListener('pointerleave', leave);

  return () => {
    unsubscribe();
    node.removeEventListener('pointermove', move);
    node.removeEventListener('pointerleave', leave);
  };
}

/**
 * Wire every `[data-magnetic]` in a subtree, the static-surface spelling.
 *
 * `data-magnetic` may carry `strength,max` as a comma pair ("0.4,20"); an
 * empty attribute takes the defaults. A malformed number falls back rather
 * than throwing: the attribute is authored in a template, and a typo in an
 * ornament must not take the page's other engines down with it.
 */
export function observeMagnetic(root: ParentNode = document): () => void {
  const disposers: (() => void)[] = [];
  for (const el of root.querySelectorAll<HTMLElement>('[data-magnetic]')) {
    const [s, m] = (el.dataset.magnetic ?? '').split(',');
    const strength = Number.parseFloat(s);
    const max = Number.parseFloat(m);
    disposers.push(
      mountMagnetic(el, {
        strength: Number.isFinite(strength) ? strength : undefined,
        max: Number.isFinite(max) ? max : undefined,
      }),
    );
  }
  return () => {
    for (const dispose of disposers) dispose();
  };
}
