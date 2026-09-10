// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Smooth scroll, once, for all three surfaces.
 *
 * There were three Lenis initialisations before this file — marketing
 * (`script/scroll.js`), docs (`SmoothScroll.astro`) and the console
 * (`kit/lib/actions.ts` `initLenis`) — with the same three options typed out
 * three times, three private rAF loops, and, worse, TWO different global names:
 * marketing and docs published `window.lenis`, the console published
 * `window.__lenis`. Anything wanting to reach the scroller (a nav "back to top",
 * an overlay that must freeze the page behind it, a docs pane that must not be
 * hijacked) therefore had to know which site it was on. `overlay-stack.ts` read
 * `__lenis`; `Nav.astro` read `lenis`; both are the same intent.
 *
 * WHY `__lenis` WON and not the shorter `lenis`: `window.lenis` is an
 * unprefixed global on a shared namespace, and the DOM makes that dangerous in
 * a way that is easy to miss — any element with `id="lenis"` or `name="lenis"`
 * is auto-exposed as `window.lenis` by the named-access-on-window rule, so a
 * single stray id in a CMS block or a third-party embed shadows the scroller and
 * every `window.lenis?.stop()` silently becomes a no-op on an HTMLElement. The
 * double underscore also reads as "internal, do not depend on this from outside
 * the codebase", which is exactly the contract: it exists so sibling modules can
 * find the instance without importing it, not as a public API. The migration
 * cost was three writers and seven readers, all in this repo, all in this PR.
 */

import Lenis, { type LenisOptions } from 'lenis';

import { prefersReducedMotion } from './motion-query';
import { subscribe } from './raf-loop';

/**
 * Interpolation factor, 0..1, lower is smoother. Lenis's own default is 0.1 and
 * this matches it deliberately rather than by omission: the value is named here
 * because it used to be typed out at three call sites, and a named constant is
 * what makes a future change one edit instead of three.
 *
 * 0.1 replaced an older `duration` + `easing` pair (Lenis supports either, not
 * both). The pair produced a fixed-length animation per wheel event, so fast
 * repeated wheeling queued visibly lagging catch-up; lerp is frame-rate relative
 * and absorbs a burst into one continuous glide. Values above ~0.15 read as
 * "barely smoothed" and below ~0.07 the page keeps drifting after the pointer
 * stops, which testers reported as the scroll being stuck.
 */
const LERP = 0.1;

type LenisWindow = Window & { __lenis?: Lenis };

/**
 * The per-surface knobs. Everything else (lerp, `smoothWheel`, `syncTouch`) is
 * fixed by this module so the three sites cannot drift apart again.
 *
 * `prevent` is docs': Lenis preventDefaults wheel and touch at the document
 * level, which kills native overflow scrolling inside every nested pane, so
 * Starlight's sidebars and any `<dialog>` have to be handed back to the browser.
 * `virtualScroll` is marketing's: the encryption scene damps the wheel delta
 * near a snap point.
 */
export type SmoothScrollOptions = Pick<LenisOptions, 'prevent' | 'virtualScroll'>;

export type SmoothScroll = {
    lenis: Lenis;
    /** Stop the rAF subscription, drop the global, destroy the instance. */
    destroy: () => void;
};

/**
 * Start smooth scrolling, or adopt the instance that is already running.
 *
 * Returns `null` — not a stub object — when smooth scroll must not run:
 * reduced motion, or no `window` (SSR). A caller that needs a scroll oracle
 * regardless has to say so with its own fallback rather than get one silently;
 * see `web/marketing/src/script/scroll.js`, which supplies a native-scroll
 * controller for the encryption scene.
 *
 * Idempotent by design, because the surfaces that call it are single-page apps
 * with a persistent `<body>`: Astro's ClientRouter re-runs module scripts after
 * a swap, and a second `new Lenis()` over the same document leaves two
 * instances fighting for `scrollTop`, which reads as the page stuttering
 * backwards. A second call returns the LIVE instance with a no-op `destroy`, so
 * the second caller cannot tear down the first caller's scroller either.
 */
export function createSmoothScroll(options: SmoothScrollOptions = {}): SmoothScroll | null {
    if (typeof window === 'undefined') return null;

    const existing = (window as LenisWindow).__lenis;
    if (existing) return { lenis: existing, destroy: () => {} };

    if (prefersReducedMotion()) return null;

    const lenis = new Lenis({
        lerp: LERP,
        smoothWheel: true,
        syncTouch: false,
        ...options,
    });

    (window as LenisWindow).__lenis = lenis;

    // No `false` return: the scroller is ticked for as long as it is mounted.
    // Lenis is already cheap when idle (it early-outs when the animation has
    // no work), and settling it here would mean waking it from wheel, touch,
    // keyboard and programmatic `scrollTo`, i.e. re-implementing its input
    // layer outside it.
    const unsubscribe = subscribe((now) => {
        lenis.raf(now);
    });

    return {
        lenis,
        destroy() {
            unsubscribe();
            if ((window as LenisWindow).__lenis === lenis) delete (window as LenisWindow).__lenis;
            lenis.destroy();
        },
    };
}

/**
 * The running scroller, if any. For the readers that only want to nudge it —
 * `stop()` behind an overlay, `scrollTo(0)` on navigation — and would otherwise
 * hand-roll the `window as unknown as { __lenis?: ... }` cast, which is how the
 * two spellings of the global survived as long as they did.
 */
export function getSmoothScroll(): Lenis | undefined {
    if (typeof window === 'undefined') return undefined;
    return (window as LenisWindow).__lenis;
}
