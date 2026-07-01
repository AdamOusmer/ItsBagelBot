/**
 * Small shared micro-interactions wired by data-attributes, so section
 * components only add markup — no per-component scripts.
 *
 *   [data-magnetic]        primary CTA drifts toward the cursor on hover.
 *                          Optional numeric value = strength (default 0.3).
 *                          Reads via `--magnetic-x/y` (see style.css). Fine
 *                          pointer + motion-allowed only.
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

function setupMagnetic(el) {
    if (el.dataset.magneticReady === "true") return;
    el.dataset.magneticReady = "true";

    const strength = parseFloat(el.dataset.magnetic) || 0.3;

    const reset = () => {
        el.style.setProperty("--magnetic-x", "0px");
        el.style.setProperty("--magnetic-y", "0px");
    };

    el.addEventListener("pointermove", (event) => {
        if (!finePointer.matches || reduceMotion.matches) return;
        const rect = el.getBoundingClientRect();
        const x = (event.clientX - (rect.left + rect.width / 2)) * strength;
        const y = (event.clientY - (rect.top + rect.height / 2)) * strength;
        el.style.setProperty("--magnetic-x", `${x.toFixed(2)}px`);
        el.style.setProperty("--magnetic-y", `${y.toFixed(2)}px`);
    });

    el.addEventListener("pointerleave", reset);
    el.addEventListener("blur", reset);
}

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
    document.querySelectorAll("[data-magnetic]").forEach(setupMagnetic);
    document.querySelectorAll("[data-copy]").forEach(setupCopy);
    document.querySelectorAll("[data-tilt]").forEach(setupTilt);
}

setup();
document.addEventListener("astro:page-load", setup);
