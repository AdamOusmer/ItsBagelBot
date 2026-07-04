/**
 * Decode-on-view text — the brand's "decrypt" reveal, reused from the
 * encryption scene as a shared utility. Tag an element `data-decode`; the
 * first time it scrolls into view its text scrambles, then resolves
 * character-by-character. Honors reduced-motion (shows final text instantly).
 *
 * The scramble plays whenever a fresh Astro page enters. In-flight work is
 * cancelled before swaps so the outgoing page cannot keep animating against
 * the incoming DOM.
 */

const SCRAMBLE = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#$%&*+-/<>";
const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

// In-flight scramble frames + observers, tracked so a navigation can cancel
// them instead of leaving them to run against the incoming page.
const runningFrames = new Set();
const observers = new Set();

function cancelAll() {
    runningFrames.forEach((id) => cancelAnimationFrame(id));
    runningFrames.clear();
    observers.forEach((observer) => observer.disconnect());
    observers.clear();
}

function runDecode(el, text) {
    if (reduceMotion.matches) {
        el.textContent = text;
        return;
    }

    const chars = Array.from(text);
    const duration = Math.min(1000, 380 + chars.length * 26);
    const start = performance.now();
    let frameId;

    function tick(now) {
        runningFrames.delete(frameId);
        const progress = Math.min(1, (now - start) / duration);
        const revealCount = Math.floor(chars.length * progress);
        const t = Math.floor(progress * 22);

        el.textContent = chars
            .map((char, i) => {
                if (char === " " || /\W/.test(char)) return char;
                if (i < revealCount) return char;
                return SCRAMBLE[(i * 19 + t * 7) % SCRAMBLE.length];
            })
            .join("");

        if (progress < 1) {
            frameId = requestAnimationFrame(tick);
            runningFrames.add(frameId);
        } else {
            el.textContent = text;
        }
    }

    frameId = requestAnimationFrame(tick);
    runningFrames.add(frameId);
}

function setupDecode(el) {
    if (el.dataset.decodeReady === "true") return;
    el.dataset.decodeReady = "true";

    const text = (el.dataset.decode && el.dataset.decode.length ? el.dataset.decode : el.textContent) ?? "";

    const observer = new IntersectionObserver(
        (entries) => {
            entries.forEach((entry) => {
                if (!entry.isIntersecting) return;
                observer.unobserve(entry.target);
                observers.delete(observer);
                runDecode(entry.target, text);
            });
        },
        { threshold: 0.45 },
    );

    observers.add(observer);
    observer.observe(el);
}

function setup() {
    document.querySelectorAll("[data-decode]").forEach(setupDecode);
}

setup();
document.addEventListener("astro:before-swap", cancelAll);
document.addEventListener("astro:page-load", setup);
