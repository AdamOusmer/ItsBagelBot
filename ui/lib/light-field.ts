// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The drifting mote field, once. The marketing site and the console each had
 * their own copy of these physics (web/marketing/src/script/lightfield.js and
 * LightField.svelte beside this file), and "same look on both surfaces" is a
 * promise a second copy cannot keep: the two had already diverged on when a
 * mote is counted warm.
 *
 * Framework-neutral on purpose, in the same spirit as rehearsal.ts: it takes a
 * host element and hands back a teardown, so Astro drives it from a DOM scan
 * with ClientRouter teardown and Svelte drives it from onMount, and neither
 * shape leaks into the other. Pure browser APIs, no imports, so both bundlers
 * inline it with no install step.
 *
 * Lives in the standalone @bagel/ui library at the repo root, not in web/kit,
 * because it is a design primitive with no bot knowledge in it and kit is the
 * bot-specific glue. Every consumer reaches it the same way now -- as the
 * package import `@bagel/ui/lib/light-field` -- where it used to be a Vite
 * alias for marketing and a relative path for the console.
 *
 * That cost the console images their narrow build context: they built with
 * `web/` as the whole context, and nothing above that line can be COPYed. They
 * now build from the repo root behind a per-image whitelist ignorefile
 * (web/dashboard/Containerfile.ignore) that admits `ui` and the parts of `web`
 * the SSR image needs, and nothing else -- the Go tree included. The whitelist
 * is the price of the library being extractable to its own repo later; a
 * `ui/` nested inside `web/` would have kept the old context and given up the
 * separate lockfile, the separate CI job and the clean lift-out.
 *
 * Each mote is a span moved by a Web Animations transform, not an arc
 * repainted into a <canvas> every frame. Measured 2026-09-23 in a WKWebView at
 * 390x844, device scale 3 (WebContent + GPU process footprint): one
 * full-screen 2D canvas redrawn per frame costs ~220 MB against ~55 MB drawn
 * once, because WebKit cycles a pool of IOSurface display buffers for a canvas
 * that repaints every frame. Capping the backing store at 1x (~110 MB) and
 * willReadFrequently (CPU canvas, no change) did not fix it; dropping to 15
 * fps only halved it. Three fields took the marketing home page to ~450 MB
 * idle and ~650 MB scrolling. The same motes as compositor-driven spans cost
 * ~40 MB total and no main-thread work per frame. Do not move this back onto
 * a per-frame canvas.
 */

import { prefersReducedMotion } from './motion-query';

/** One drifting speck. Speeds are px per 60 Hz frame, the unit the field was tuned in. */
type Mote = {
    r: number;
    vx: number;
    vy: number;
    alpha: number;
    warm: boolean;
};

export type FieldOptions = {
    /** Share of gold (vs green) motes, 0..1. */
    warmth?: number;
};

const FRAME_MS = 1000 / 60;
const EDGE_PX = 10;

/**
 * Start a mote field in `host`. Returns the teardown, or `null` when the
 * field did not start (reduced motion, or no Web Animations) so a caller that
 * tracks "this node is wired" can tell the difference.
 *
 * The motes only exist while the host is near the viewport: an off-screen
 * field is dozens of composited layers of cost and none of its value. The
 * 150px root margin builds it just before it scrolls in, so it is never caught
 * with an empty host.
 */
export function field(host: HTMLElement, options: FieldOptions = {}): (() => void) | null {
    if (prefersReducedMotion()) return null;
    if (typeof host.animate !== 'function') return null;

    const warmth = options.warmth ?? 0.7;
    let width = 0;
    let height = 0;
    let visible = false;
    const animations = new Set<Animation>();

    function clear() {
        for (const animation of animations) animation.cancel();
        animations.clear();
        host.replaceChildren();
    }

    // Up-and-out re-enters from the bottom at a fresh x, as the per-frame
    // version did; one restart per mote every 1-5 minutes is the whole JS cost.
    function fly(dot: HTMLElement, mote: Mote, fromY: number) {
        const x = Math.random() * width - mote.r;
        const frames = (fromY + EDGE_PX) / mote.vy;
        const animation = dot.animate(
            [
                { transform: `translate(${x}px, ${fromY - mote.r}px)` },
                { transform: `translate(${x + mote.vx * frames}px, ${-EDGE_PX - mote.r}px)` },
            ],
            { duration: frames * FRAME_MS, easing: 'linear' },
        );
        animations.add(animation);
        animation.onfinish = () => {
            animations.delete(animation);
            fly(dot, mote, height + EDGE_PX);
        };
    }

    function build() {
        clear();
        width = host.clientWidth;
        height = host.clientHeight;
        if (!width || !height) return;
        for (const mote of makeMotes(width, warmth)) {
            const dot = document.createElement('span');
            dot.style.width = dot.style.height = `${mote.r * 2}px`;
            dot.style.background = `rgba(${mote.warm ? '201, 168, 124' : '82, 183, 136'}, ${mote.alpha.toFixed(3)})`;
            host.append(dot);
            fly(dot, mote, Math.random() * height);
        }
    }

    const observer = new IntersectionObserver(([entry]) => {
        visible = entry.isIntersecting;
        if (visible) build();
        else clear();
    }, { rootMargin: '150px' });

    const resizer = new ResizeObserver(() => {
        if (visible && (host.clientWidth !== width || host.clientHeight !== height)) build();
    });

    observer.observe(host);
    resizer.observe(host);

    return () => {
        observer.disconnect();
        resizer.disconnect();
        clear();
    };
}

function makeMotes(width: number, warmth: number): Mote[] {
    const count = width < 700 ? 40 : 70;
    return Array.from({ length: count }, () => ({
        r: 0.6 + Math.random() * 2,
        vy: 0.05 + Math.random() * 0.2,
        vx: (Math.random() - 0.5) * 0.1,
        alpha: 0.12 + Math.random() * 0.45,
        warm: Math.random() < warmth,
    }));
}
