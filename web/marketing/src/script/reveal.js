// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { observeReveal } from '@bagel/ui/lib/reveal';
import { getLangSwitchScrollY } from '../i18n/route-transition';

let dispose = null;

function teardown() {
    dispose?.();
    dispose = null;
}

function setup() {
    teardown();
    dispose = observeReveal(document, {instant: getLangSwitchScrollY() !== null});
}

setup();
document.addEventListener('astro:page-load', setup);
document.addEventListener('astro:before-swap', teardown);
window.addEventListener('pagehide', teardown);
