/**
 * Small shared micro-interactions wired by data-attributes, so section
 * components only add markup — no per-component scripts.
 *
 *
 *   [data-copy="text"]     click copies `text` to the clipboard and toggles
 *                          `.is-copied` for ~1.6s of micro-feedback.
 *
 *   [data-tilt]            pointer-tracked 3D tilt. Optional numeric value =
 *                          max degrees (default 4). Writes `--tilt-x/y`; the
 *                          component's CSS applies the perspective transform.
 *                          Fine pointer + motion-allowed only.
 */

const finePointer = window.matchMedia("(hover: hover) and (pointer: fine)");
const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");


function setupCopy(el) {
    if (el.dataset.copyReady === "true") return;
    el.dataset.copyReady = "true";

    el.addEventListener("click", async (event) => {
        const text = el.dataset.copy;
        if (!text || !navigator.clipboard) return;

        event.preventDefault();
        event.stopPropagation();
        try {
            await navigator.clipboard.writeText(text);
            el.classList.add("is-copied");
            window.clearTimeout(el._copyTimer);
            el._copyTimer = window.setTimeout(() => el.classList.remove("is-copied"), 1600);
        } catch {
            /* clipboard blocked — leave the element as-is */
        }
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
