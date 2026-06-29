/**
 * Decode-on-view text — the brand's "decrypt" reveal, reused from the
 * encryption scene as a shared utility. Tag an element `data-decode`; the
 * first time it scrolls into view its text scrambles, then resolves
 * character-by-character. Honors reduced-motion (shows final text instantly).
 */

const SCRAMBLE = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#$%&*+-/<>";
const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

function runDecode(el, text) {
    if (reduceMotion.matches) {
        el.textContent = text;
        return;
    }

    const chars = Array.from(text);
    const duration = Math.min(1000, 380 + chars.length * 26);
    const start = performance.now();

    function tick(now) {
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

        if (progress < 1) requestAnimationFrame(tick);
        else el.textContent = text;
    }

    requestAnimationFrame(tick);
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
                runDecode(entry.target, text);
            });
        },
        { threshold: 0.45 },
    );

    observer.observe(el);
}

function setup() {
    document.querySelectorAll("[data-decode]").forEach(setupDecode);
}

setup();
document.addEventListener("astro:page-load", setup);
