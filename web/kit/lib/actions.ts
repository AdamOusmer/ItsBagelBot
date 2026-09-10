// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Client behaviors shared by both consoles.
import type { Action } from 'svelte/action';

import { hasFinePointer, prefersReducedMotion } from '@bagel/ui/lib/motion-query';
import { subscribe, wake, type Tick } from '@bagel/ui/lib/raf-loop';

type MagneticOpts = { strength?: number; max?: number };

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

/**
 * design.google-style magnetic hover: while the pointer is over the element it
 * eases toward the cursor by `strength` of the offset from center, capped at
 * `max` px. Releases back to rest on leave. Respects reduced-motion and skips
 * coarse pointers. Use as `use:magnetic` on buttons/links.
 *
 * Ticked by @bagel/ui's one shared scheduler rather than its own
 * requestAnimationFrame: a toolbar of magnetic buttons used to register one
 * loop per button, each reading `getBoundingClientRect` in its own callback
 * inside the same frame.
 *
 * The pointer gate widened from `(pointer: fine)` to `(hover: hover) and
 * (pointer: fine)` when it moved onto the shared query. Deliberate: a stylus
 * reports `pointer: fine` and cannot hover, so it got a magnetic offset it
 * could never see resting.
 */
export const magnetic: Action<HTMLElement, MagneticOpts | undefined> = (node, opts) => {
  if (!hasFinePointer() || prefersReducedMotion()) return;

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

  return {
    destroy() {
      unsubscribe();
      node.removeEventListener('pointermove', move);
      node.removeEventListener('pointerleave', leave);
    }
  };
};

/* Count-up lives in @bagel/ui/lib/count-up now: it is a plain
 * mount-and-teardown over textContent with no bot knowledge in it, and
 * StatTile -- its only caller -- moved to the library with it. Re-exported
 * rather than dropped so `import { countUp } from '@bagel/kit'` and
 * '@bagel/kit/actions' keep resolving; there is no second implementation. */
export { countUp } from '@bagel/ui/lib/count-up';

/**
 * Smooth scroll for the console shell. Returns a teardown; call it from
 * `onMount` and let the returned function be the effect's cleanup. No-op under
 * reduced motion, and a no-op teardown if a scroller was already running.
 *
 * The config, the `window.__lenis` publication and the rAF tick all live in
 * `@bagel/ui/lib/lenis` now — this is the console's thin, still-async wrapper.
 *
 * Still a dynamic import, and that is the point of the wrapper: it keeps
 * `lenis` (about 20 KB) out of the shell's initial chunk on a surface whose
 * first paint is a dashboard, not a scroll. A static import here would put the
 * library in the entry for every console route, including the ones that do not
 * scroll.
 */
export async function initLenis(): Promise<() => void> {
  if (typeof window === 'undefined') return () => {};
  const { createSmoothScroll } = await import('@bagel/ui/lib/lenis');
  return createSmoothScroll()?.destroy ?? (() => {});
}
