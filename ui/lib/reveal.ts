// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Scroll reveal: the entrance every section on every surface shares.
 *
 * An element tagged `data-reveal` starts hidden (opacity 0 plus a small
 * translate, from the CSS contract) and gains `is-revealed` the first time it
 * scrolls into view. Neighbours stagger with an inline `--reveal-i: N`.
 *
 * From web/marketing/src/script/reveal.js, which replaced an older
 * `animation: fadeUp ... forwards` pattern. That is worth keeping written down,
 * because the old pattern is what anyone reaching for "just animate it" writes
 * again: a keyframe on load fires for sections that are three screens below the
 * fold, so by the time the visitor scrolls there the entrance has already
 * played to an empty viewport and the section is simply present. Reveal is
 * viewport-driven, so the choreography happens where it can be seen.
 *
 * An IntersectionObserver rather than a scroll listener, and not for tidiness:
 * a scroll handler that measures N elements runs on the main thread inside the
 * scroll, which is the classic way to make a smooth-scrolled page stutter. The
 * observer is off-main-thread until something actually crosses.
 *
 * Framework-free. The marketing site drives it from a DOM scan with
 * ClientRouter teardown; Svelte drives it from the `reveal` action in
 * ui/svelte/actions.ts.
 */

import { prefersReducedMotion } from './motion-query';

const SELECTOR = '[data-reveal]';
const REVEALED = 'is-revealed';

export type RevealOptions = {
    /**
     * Default `0px 0px -10% 0px`: the bottom edge of the root is pulled UP by
     * a tenth of the viewport, so an element has to be a little way in before
     * it counts. Revealing exactly at the edge means the entrance plays
     * off-screen on a fast scroll and the element is already settled by the
     * time it is readable.
     */
    rootMargin?: string;
    /**
     * Default `0.08`. Low on purpose: sections here are frequently taller than
     * the viewport, and any threshold near 0.5 can never be reached by one, so
     * it would simply never fire. 8% is "the top edge is properly in", not a
     * fraction of the element.
     */
    threshold?: number;
};

const DEFAULTS: Required<RevealOptions> = {
    rootMargin: '0px 0px -10% 0px',
    threshold: 0.08,
};

function onIntersect(entries: IntersectionObserverEntry[], observer: IntersectionObserver): void {
    for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const el = entry.target as HTMLElement;
        el.classList.add(REVEALED);
        // `data-reveal-repeat` keeps the element observed so a later crossing
        // fires again. Nothing here ever REMOVES the class: a repeat element
        // owns its own exit, because "hide it again on the way out" is a
        // different effect and one that reads as flicker on a short section.
        if (el.dataset.revealRepeat !== 'true') observer.unobserve(el);
    }
}

/** Elements under (or equal to) `root` that are not already wired. */
function targetsIn(root: ParentNode): HTMLElement[] {
    const found = Array.from(root.querySelectorAll<HTMLElement>(SELECTOR));
    // A Svelte `use:reveal` is placed ON the element as often as on a wrapper,
    // and querySelectorAll never returns its own root.
    if (root instanceof HTMLElement && root.matches(SELECTOR)) found.unshift(root);
    return found.filter((el) => el.dataset.revealReady !== 'true');
}

/** Already in the viewport right now. */
function onScreen(el: HTMLElement): boolean {
    const rect = el.getBoundingClientRect();
    return rect.top < window.innerHeight && rect.bottom > 0;
}

/**
 * Wire every `[data-reveal]` under `root`. Returns the teardown.
 *
 * Idempotent through a `data-reveal-ready` attribute, so a host may call it
 * again after a DOM swap without double-observing what survived. The flag is a
 * DOM attribute rather than a WeakSet for the same reason bindOnce's is: it is
 * observable from a test.
 *
 * The teardown CLEARS that flag on everything it wired, and it has to. The
 * marketing host disposes and re-scans on every `astro:page-load`, and that
 * event also fires once on a cold load, right after this module's own first
 * call. With the flag left set, the second scan filters out all 50 elements on
 * the homepage as "already wired", observes nothing, and the site ships with
 * scroll reveal silently dead — measured, not hypothesised: 0 of 50 elements
 * revealed after a 5589px scroll. A flag that outlives the observer it stands
 * for is not idempotence, it is a stale lock.
 *
 * Under reduced motion (or with no IntersectionObserver at all) everything is
 * revealed immediately and no observer is created. That is the whole
 * accessibility story of this effect: the content is never gated on motion the
 * visitor asked not to see.
 */
export function observeReveal(root: ParentNode, options: RevealOptions = {}): () => void {
    const { rootMargin, threshold } = { ...DEFAULTS, ...options };
    const targets = targetsIn(root);
    for (const el of targets) el.dataset.revealReady = 'true';

    const release = (): void => {
        for (const el of targets) delete el.dataset.revealReady;
    };

    if (prefersReducedMotion() || typeof IntersectionObserver === 'undefined') {
        for (const el of targets) el.classList.add(REVEALED);
        return release;
    }

    const observer = new IntersectionObserver(onIntersect, { rootMargin, threshold });
    for (const el of targets) {
        // Above the fold at wiring time: reveal now so it plays its entrance,
        // instead of waiting for a scroll that on a short page never comes.
        if (onScreen(el)) el.classList.add(REVEALED);
        else observer.observe(el);
    }

    return () => {
        observer.disconnect();
        release();
    };
}
