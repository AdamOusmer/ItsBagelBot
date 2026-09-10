// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Small shared micro-interactions wired by data-attributes, so section
 * components only add markup, no per-component scripts.
 *
 *
 *   [data-copy="text"]     click copies `text` to the clipboard and toggles
 *                          `.is-done` (the .bb-chip confirmed state) for the
 *                          library's shared flash duration
 *                          (@bagel/ui/lib/clipboard, 1600ms).
 *
 *   [data-tilt]            pointer-tracked 3D tilt. Optional numeric value =
 *                          max degrees (default 4). Writes `--tilt-x/y`; the
 *                          component's CSS applies the perspective transform.
 *                          Fine pointer + motion-allowed only.
 */

import { copyFlash } from '@bagel/ui/lib/clipboard';
import { finePointer, reduceMotion } from './motion';

/**
 * Which copy is currently allowed to own an element's flash.
 *
 * `copyFlash` owns one timer per call, so two clicks 200ms apart schedule two
 * clear-outs and the FIRST one lands while the second copy is still fresh —
 * the confirmation blinks off 1.4s early. The old code avoided that by
 * clearing its own pending timer; the shared helper cannot, because a per-
 * element timer registry is caller state and does not belong in a two-line
 * clipboard wrapper. A sequence number is the same fix from the other side:
 * a stale callback simply finds it is no longer the current copy and does
 * nothing.
 *
 * A WeakMap rather than an expando so a removed element takes its entry with
 * it, and so `checkJs` does not have to be told about a property on Element.
 */
const copySeq = new WeakMap();

function setupCopy(el) {
    if (el.dataset.copyReady === "true") return;
    el.dataset.copyReady = "true";

    el.addEventListener("click", (event) => {
        const text = el.dataset.copy;
        if (!text || !navigator.clipboard) return;

        event.preventDefault();
        event.stopPropagation();

        const seq = (copySeq.get(el) ?? 0) + 1;
        copySeq.set(el, seq);
        copyFlash(text, (on) => {
            if (copySeq.get(el) !== seq) return;
            el.classList.toggle("is-done", on);
        });
    });
}

function setupTilt(el) {
    if (el.dataset.tiltReady === "true") return;
    el.dataset.tiltReady = "true";

    const maxDeg = parseFloat(el.dataset.tilt) || 4;

    const reset = () => {
        el.style.setProperty("--tilt-x", "0deg");
        el.style.setProperty("--tilt-y", "0deg");
    };

    el.addEventListener("pointermove", (event) => {
        if (!finePointer.matches || reduceMotion.matches) return;
        const rect = el.getBoundingClientRect();
        const px = (event.clientX - rect.left) / rect.width - 0.5;
        const py = (event.clientY - rect.top) / rect.height - 0.5;
        el.style.setProperty("--tilt-x", `${(-py * maxDeg).toFixed(2)}deg`);
        el.style.setProperty("--tilt-y", `${(px * maxDeg).toFixed(2)}deg`);
    });

    el.addEventListener("pointerleave", reset);

    reset();
}

function setup() {
    document.querySelectorAll("[data-copy]").forEach(setupCopy);
    document.querySelectorAll("[data-tilt]").forEach(setupTilt);
}

setup();
document.addEventListener("astro:page-load", setup);
