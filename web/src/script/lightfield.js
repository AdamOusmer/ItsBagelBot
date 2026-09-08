// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Standalone warm light-field for non-scrubbed surfaces (inner-page heroes).
 * Any `<canvas data-field>` gets the same drifting mote field the cinematic
 * scenes use, at a constant warmth.
 *
 * The physics live in `@bagel/light-field` (console/shared/lib/light-field.ts),
 * shared with the console's LightField.svelte so the two surfaces cannot drift
 * apart on how the field looks. What stays here is everything Astro-shaped: the
 * DOM scan, the ready flag, and teardown on `astro:before-swap`.
 *
 * This does NOT use bindOnce: a field owns a rAF loop and an
 * IntersectionObserver, so it needs a real teardown before the ClientRouter
 * throws the old document away, and bindOnce deliberately has no unbind.
 */

import { field } from '@bagel/light-field';

const activeFieldCleanups = new Set();

function setupField(canvas) {
    if (canvas.dataset.fieldReady === 'true') return;

    const stop = field(canvas, { warmth: parseFloat(canvas.dataset.warmth) || 0.7 });
    if (!stop) return;
    canvas.dataset.fieldReady = 'true';

    const cleanup = () => {
        stop();
        canvas.removeAttribute('data-field-ready');
        // Zeroing the backing store frees the (dpr-scaled) bitmap right away
        // instead of waiting for the detached canvas to be collected.
        canvas.width = 0;
        canvas.height = 0;
        activeFieldCleanups.delete(cleanup);
    };
    activeFieldCleanups.add(cleanup);
}

function setup() {
    document.querySelectorAll('canvas[data-field]').forEach(setupField);
}

function cleanupAll() {
    activeFieldCleanups.forEach((cleanup) => cleanup());
}

setup();
document.addEventListener('astro:page-load', setup);
document.addEventListener('astro:before-swap', cleanupAll);
window.addEventListener('pagehide', cleanupAll);
