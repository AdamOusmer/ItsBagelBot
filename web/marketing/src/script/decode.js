// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
