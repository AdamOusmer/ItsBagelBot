// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Off-screen motion gate for the `[data-motion]` sections, rescanned on every
// ClientRouter navigation like reveal.js.

import { observeMotion } from '@bagel/ui/lib/motion-gate';

let dispose = null;

function teardown() {
    dispose?.();
    dispose = null;
}

function setup() {
    teardown();
    dispose = observeMotion(document);
}

setup();
document.addEventListener('astro:page-load', setup);
document.addEventListener('astro:before-swap', teardown);
window.addEventListener('pagehide', teardown);
