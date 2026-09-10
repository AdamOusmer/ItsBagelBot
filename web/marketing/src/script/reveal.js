// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Scroll reveal, Astro-shaped.
 *
 * The observer, the reduced-motion branch, the above-the-fold immediate reveal
 * and the `data-reveal-repeat` opt-in all live in `@bagel/ui/lib/reveal` now,
 * shared with the console. What is left here is the part that is genuinely
 * about this site: it scans the whole document, and it does that again on every
 * ClientRouter navigation.
 *
 * Document-wide on purpose, where the console uses the `use:reveal` action on
 * the sections that want it. Practically every section of this site is a
 * reveal, so one observer for the page is cheaper than one per section; on the
 * console, reveals are the exception and a page-wide scan would mostly find
 * nothing.
 *
 * The previous observer is disposed before the next scan rather than kept for
 * the life of the tab. A ClientRouter swap replaces the whole document body, so
 * everything the old observer was watching is detached — keeping it would leak
 * one live observer per navigation, watching nodes that can never intersect
 * anything again.
 */

import { observeReveal } from '@bagel/ui/lib/reveal';

let dispose = null;

function teardown() {
    dispose?.();
    dispose = null;
}

function setup() {
    teardown();
    dispose = observeReveal(document);
}

setup();
document.addEventListener('astro:page-load', setup);
document.addEventListener('astro:before-swap', teardown);
window.addEventListener('pagehide', teardown);
