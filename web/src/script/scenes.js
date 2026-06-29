/**
 * Scroll-scene engine — the spine of the homepage journey.
 *
 * Each `[data-scene]` is a tall section (e.g. height: 400vh) wrapping a
 * `position: sticky` pin. As you scroll through it, this writes a smoothed
 * progress `--p` (0 → 1) onto the element. Components react in pure CSS via
 * `calc()` on `--p`, so the whole journey scrubs to scroll with no per-frame
 * work in the components themselves.
 *
 * Reads Lenis' smoothed scroll when present so it shares the same eased
 * trajectory as the rest of the site. Reduced-motion snaps progress (no lerp).
 */

const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

let scenes = [];
let rafId = 0;

function measure(scene) {
    let top = 0;
    let node = scene.el;
    while (node) {
        top += node.offsetTop;
        node = node.offsetParent;
    }
    scene.top = top;
    scene.range = Math.max(1, scene.el.offsetHeight - window.innerHeight);
}

function collect() {
    scenes = Array.from(document.querySelectorAll("[data-scene]")).map((el) => {
        const scene = { el, top: 0, range: 1, p: 0, target: 0 };
        measure(scene);
        return scene;
    });
}

function scrollPosition() {
    const lenis = window.lenis;
    if (lenis && typeof lenis.scroll === "number" && Number.isFinite(lenis.scroll)) {
        return lenis.scroll;
    }
    return window.scrollY;
}

function frame() {
    const y = scrollPosition();
    const snap = reduceMotion.matches;

    for (const scene of scenes) {
        scene.target = Math.min(1, Math.max(0, (y - scene.top) / scene.range));
        scene.p = snap ? scene.target : scene.p + (scene.target - scene.p) * 0.12;
        if (Math.abs(scene.target - scene.p) < 0.0002) scene.p = scene.target;
        scene.el.style.setProperty("--p", scene.p.toFixed(4));
    }

    rafId = requestAnimationFrame(frame);
}

function init() {
    collect();
    if (!scenes.length) return;
    cancelAnimationFrame(rafId);
    frame();
}

window.addEventListener("resize", () => scenes.forEach(measure), { passive: true });
window.addEventListener("load", () => scenes.forEach(measure), { passive: true });
document.fonts?.ready.then(() => scenes.forEach(measure)).catch(() => {});

init();
document.addEventListener("astro:page-load", init);
