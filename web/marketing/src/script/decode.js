// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Decode-on-view text, Astro-shaped.
 *
 * The scramble alphabet, the length-scaled duration, the noise clock, the
 * reduced-motion branch and the frame subscription all live in
 * `@bagel/ui/lib/decode` now, shared with the console. What is left here is the
 * part that is genuinely about this site: it scans the whole document, and it
 * does that again on every ClientRouter navigation.
 *
 * Document-wide rather than per-component, and that is why this file still
 * exists instead of PageHero hoisting its own script: `data-decode` is carried
 * by the page hero's title AND by the homepage's step receipts, and the
 * homepage renders no hero. A per-element script would wire nothing on one page
 * and wire the same elements twice on any page that grew both.
 *
 * The previous run is disposed before the next scan rather than kept for the
 * life of the tab. A swap mid-scramble would otherwise leave a frame callback
 * writing textContent into a detached node until its own clock expires, which
 * is what the old implementation tracked a module-level Set of frame ids for.
 */

import { observeDecode } from '@bagel/ui/lib/decode';

let dispose = null;

function teardown() {
    dispose?.();
    dispose = null;
}

function setup() {
    teardown();
    dispose = observeDecode(document);
}

setup();
document.addEventListener('astro:page-load', setup);
document.addEventListener('astro:before-swap', teardown);
window.addEventListener('pagehide', teardown);
