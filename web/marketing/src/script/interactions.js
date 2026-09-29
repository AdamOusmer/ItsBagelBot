// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { copyFlash } from '@bagel/ui/lib/clipboard';
import { observeTilt } from '@bagel/ui/lib/tilt';

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

function setup() {
    document.querySelectorAll("[data-copy]").forEach(setupCopy);
    observeTilt();
}

setup();
document.addEventListener("astro:page-load", setup);
