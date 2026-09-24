// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { copyFlash } from '@bagel/ui/lib/clipboard';
import { finePointer, reduceMotion } from '@bagel/ui/lib/motion-query';

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
